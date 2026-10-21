package todo

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"gowiki/backend/internal/database"
)

// dailyCapWindow is the per-(user, page) suppression window. A user
// who already got an email for this page within this long ago gets
// nothing — not an abbreviated reminder, not a nudge, nothing. The
// value is deliberately NOT configurable: the request was "no more
// than once per day", and widening or narrowing it changes the
// contract users rely on, not a knob.
const dailyCapWindow = 24 * time.Hour

// NotificationSender is the write-side of the batcher — the function
// that actually puts bytes on the wire. Separated so tests can
// substitute an in-memory recorder, and so the Dispatcher's email +
// webhook plumbing stays decoupled from the batching + capping logic.
type NotificationSender interface {
	SendBatch(user string, events []NotifyEvent)
}

// CapStore gates whether a (user, page) pair is still allowed to
// receive a notification. Thin wrapper over todo_notification_sends.
type CapStore interface {
	ShouldSuppress(ctx context.Context, userID, pageKey string, within time.Duration) bool
	RecordSend(ctx context.Context, userID, pageKey string)
}

// NotificationBatcher is the trailing-debounce + per-user grouping
// layer that sits between the todo plugin's event sources and the
// email + webhook sender.
//
// On Enqueue, the event lands in the user's pending buffer and the
// user's debounce timer is reset. When the window elapses with no
// new triggers, the buffer is drained: pages that have already been
// notified within the daily cap are dropped silently, and the rest
// collapse into a single call to the sender.
//
// Why trailing instead of leading: the goal is "wait a bit so late
// triggers can join the batch", which only trailing achieves. A
// leading-edge debounce would send immediately on the first trigger
// and swallow the later ones — the opposite of what Alice asked for.
type NotificationBatcher struct {
	mu        sync.Mutex
	pending   map[string]*userBatch
	stopped   bool
	windowFn  func() time.Duration
	cap       CapStore
	sender    NotificationSender
	nowFn     func() time.Time
	afterFunc func(time.Duration, func()) stoppableTimer
}

// stoppableTimer is the minimal timer surface the batcher needs. The
// real implementation returns *time.Timer; tests substitute a fake
// clock.
type stoppableTimer interface {
	Stop() bool
}

type userBatch struct {
	events []NotifyEvent
	timer  stoppableTimer
}

// NewNotificationBatcher builds a batcher backed by a config lookup
// for the window and a Postgres-backed CapStore for the 24h cap.
func NewNotificationBatcher(windowFn func() time.Duration, cap CapStore, sender NotificationSender) *NotificationBatcher {
	return &NotificationBatcher{
		pending:  make(map[string]*userBatch),
		windowFn: windowFn,
		cap:      cap,
		sender:   sender,
		nowFn:    time.Now,
		afterFunc: func(d time.Duration, f func()) stoppableTimer {
			return time.AfterFunc(d, f)
		},
	}
}

// Enqueue folds an event into the user's pending batch and resets
// the debounce timer. If batching is disabled (window = 0), the
// event is sent synchronously via a one-element batch.
func (b *NotificationBatcher) Enqueue(ev NotifyEvent) {
	user := batchKey(ev)
	if user == "" {
		return
	}

	b.mu.Lock()
	if b.stopped {
		b.mu.Unlock()
		return
	}

	window := b.windowFn()
	if window <= 0 {
		// Batching off: send straight through, but still enforce the
		// 24h cap so the "max once per day" contract holds. Group of
		// one.
		b.mu.Unlock()
		b.flushEvents(user, []NotifyEvent{ev})
		return
	}

	batch, ok := b.pending[user]
	if !ok {
		batch = &userBatch{}
		b.pending[user] = batch
	}
	batch.events = append(batch.events, ev)
	if batch.timer != nil {
		batch.timer.Stop()
	}
	batch.timer = b.afterFunc(window, func() {
		b.fire(user)
	})
	b.mu.Unlock()
}

// fire is the timer callback — drain the user's batch and hand the
// events to the sender via flushEvents. Runs on the timer's own
// goroutine; must not reacquire the mutex while sending.
func (b *NotificationBatcher) fire(user string) {
	b.mu.Lock()
	batch, ok := b.pending[user]
	if ok {
		delete(b.pending, user)
	}
	b.mu.Unlock()
	if !ok || batch == nil {
		return
	}
	b.flushEvents(user, batch.events)
}

// flushEvents de-dupes events by page_key (keeping the latest event
// for each page), filters out pages still within their 24h cap, and
// hands the surviving set to the sender as a single call. The cap
// records are updated after the sender returns, not before, so a
// send failure can be retried on the next trigger without the record
// pre-blocking the retry.
func (b *NotificationBatcher) flushEvents(user string, events []NotifyEvent) {
	if len(events) == 0 {
		return
	}
	// Keep one event per page_key — the most recently queued one.
	// Later events override earlier ones so a (assigned, overdue)
	// sequence lands as "overdue" in the digest.
	byKey := make(map[string]NotifyEvent, len(events))
	order := make([]string, 0, len(events))
	for _, ev := range events {
		key := pageKey(ev)
		if _, seen := byKey[key]; !seen {
			order = append(order, key)
		}
		byKey[key] = ev
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	kept := make([]NotifyEvent, 0, len(byKey))
	keptKeys := make([]string, 0, len(byKey))
	for _, key := range order {
		ev := byKey[key]
		if b.cap != nil && b.cap.ShouldSuppress(ctx, user, key, dailyCapWindow) {
			continue
		}
		kept = append(kept, ev)
		keptKeys = append(keptKeys, key)
	}
	if len(kept) == 0 {
		return
	}

	b.sender.SendBatch(user, kept)

	if b.cap != nil {
		for _, key := range keptKeys {
			b.cap.RecordSend(ctx, user, key)
		}
	}
}

// Shutdown flushes any pending batches synchronously so a graceful
// restart doesn't drop queued notifications. The batcher is marked
// stopped afterwards; late Enqueue calls become no-ops.
func (b *NotificationBatcher) Shutdown() {
	b.mu.Lock()
	b.stopped = true
	pending := b.pending
	b.pending = make(map[string]*userBatch)
	b.mu.Unlock()

	for user, batch := range pending {
		if batch.timer != nil {
			batch.timer.Stop()
		}
		b.flushEvents(user, batch.events)
	}
}

// batchKey returns the string we group notifications under. UserID
// (username) wins when set; otherwise Recipient (which may be an
// email). Empty means "unaddressable", skipped.
func batchKey(ev NotifyEvent) string {
	if ev.UserID != "" {
		return ev.UserID
	}
	return ev.Recipient
}

// pageKey returns the (per-user) suppression key for an event. A task
// with a source page uses the page path (so all notifications about
// the same doc share one cap); a bare API task falls back to the
// task ID (API tasks don't carry a page, but they still deserve the
// 24h shield).
func pageKey(ev NotifyEvent) string {
	if ev.Task == nil {
		return ""
	}
	if ev.Task.SourcePage != "" {
		return ev.Task.SourcePage
	}
	return "task:" + ev.Task.ID
}

// ── CapStore: Postgres-backed implementation ─────────────────────────

// dbCapStore implements CapStore against the todo_notification_sends
// table. Upsert pattern mirrors the other todo tables.
type dbCapStore struct {
	pool *database.Pool
}

// NewDBCapStore builds a CapStore backed by the todo_notification_sends
// table. A nil pool returns a store that disables the cap (every
// ShouldSuppress returns false, RecordSend is a no-op). Used so a
// no-database test can exercise the batcher without touching Postgres.
func NewDBCapStore(pool *database.Pool) CapStore {
	return &dbCapStore{pool: pool}
}

func (s *dbCapStore) ShouldSuppress(ctx context.Context, userID, pageKey string, within time.Duration) bool {
	if s.pool == nil || userID == "" || pageKey == "" {
		return false
	}
	p := s.pool.GetPool()
	if p == nil {
		return false
	}
	var sentAt time.Time
	err := p.QueryRow(ctx, `SELECT last_sent_at FROM todo_notification_sends WHERE user_id = $1 AND page_key = $2`, userID, pageKey).Scan(&sentAt)
	if err != nil {
		return false
	}
	return time.Since(sentAt) < within
}

func (s *dbCapStore) RecordSend(ctx context.Context, userID, pageKey string) {
	if s.pool == nil || userID == "" || pageKey == "" {
		return
	}
	p := s.pool.GetPool()
	if p == nil {
		return
	}
	_, err := p.Exec(ctx, `
		INSERT INTO todo_notification_sends (user_id, page_key, last_sent_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT (user_id, page_key)
		DO UPDATE SET last_sent_at = EXCLUDED.last_sent_at`,
		userID, pageKey)
	if err != nil {
		log.Printf("todo: record notification send for %s/%s failed: %v", userID, pageKey, err)
	}
}

// ── Reaper: purge rows older than the cap window ──────────────────

// ReapOldNotificationSends deletes todo_notification_sends rows older
// than the cap window — they're already irrelevant (a 25h-old row
// can never cause a suppression), and leaving them in forever would
// grow the table unbounded on a busy site. Idempotent; safe to run
// repeatedly.
func ReapOldNotificationSends(ctx context.Context, pool *database.Pool) (int, error) {
	p := pool.GetPool()
	if p == nil {
		return 0, fmt.Errorf("database not connected")
	}
	tag, err := p.Exec(ctx, `DELETE FROM todo_notification_sends WHERE last_sent_at < NOW() - $1::interval`,
		fmt.Sprintf("%d seconds", int(dailyCapWindow.Seconds())))
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}
