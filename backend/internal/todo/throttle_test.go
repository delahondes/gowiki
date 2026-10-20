package todo

import (
	"context"
	"sync"
	"testing"
	"time"
)

// NotificationBatcher's correctness lives in three invariants:
// (1) multiple triggers for one user within the window collapse to
//     one SendBatch call,
// (2) a per-(user, page) 24h cap holds across calls,
// (3) Shutdown flushes anything still queued, so a graceful restart
//     doesn't drop pending digests.
//
// The real batcher uses time.AfterFunc; tests substitute a fake
// afterFunc that returns a controllable timer, so a "fire the timer
// now" happens deterministically without sleeping.

type fakeTimer struct {
	fired   bool
	stopped bool
	fire    func()
}

func (t *fakeTimer) Stop() bool {
	if t.stopped || t.fired {
		return false
	}
	t.stopped = true
	return true
}

func (t *fakeTimer) trigger() {
	if t.stopped || t.fired {
		return
	}
	t.fired = true
	t.fire()
}

type recordingSender struct {
	mu    sync.Mutex
	calls []senderCall
}

type senderCall struct {
	user   string
	events []NotifyEvent
}

func (r *recordingSender) SendBatch(user string, events []NotifyEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := make([]NotifyEvent, len(events))
	copy(cp, events)
	r.calls = append(r.calls, senderCall{user: user, events: cp})
}

func (r *recordingSender) snapshot() []senderCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]senderCall, len(r.calls))
	copy(out, r.calls)
	return out
}

// memCap is an in-memory CapStore. 24h cap stored as a map from
// (user, pageKey) → last send time. ShouldSuppress is strict less-than
// so a cap-boundary trigger is NOT suppressed (identical semantics to
// the Postgres version using `time.Since(sentAt) < within`).
type memCap struct {
	mu   sync.Mutex
	last map[string]time.Time
	now  func() time.Time
}

func newMemCap() *memCap {
	return &memCap{last: map[string]time.Time{}, now: time.Now}
}

func (c *memCap) key(user, page string) string { return user + "\x00" + page }

func (c *memCap) ShouldSuppress(_ context.Context, user, page string, within time.Duration) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	t, ok := c.last[c.key(user, page)]
	if !ok {
		return false
	}
	return c.now().Sub(t) < within
}

func (c *memCap) RecordSend(_ context.Context, user, page string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.last[c.key(user, page)] = c.now()
}

// newTestBatcher wires the batcher with the injected afterFunc so
// tests can fire timers on demand. Returns the batcher, a *fakeTimer
// slot the test can inspect, a recording sender, and the in-memory
// cap.
func newTestBatcher(t *testing.T, window time.Duration) (*NotificationBatcher, *[]*fakeTimer, *recordingSender, *memCap) {
	t.Helper()
	sender := &recordingSender{}
	cap := newMemCap()
	timers := &[]*fakeTimer{}
	b := NewNotificationBatcher(func() time.Duration { return window }, cap, sender)
	b.afterFunc = func(_ time.Duration, f func()) stoppableTimer {
		ft := &fakeTimer{fire: f}
		*timers = append(*timers, ft)
		return ft
	}
	return b, timers, sender, cap
}

func makeEvent(user, page, kind string) NotifyEvent {
	return NotifyEvent{
		Type:   kind,
		UserID: user,
		Task:   &Task{ID: "task-" + page, Title: "T " + page, SourcePage: page},
	}
}

func TestBatcher_SingleTrigger_FiresAfterWindow(t *testing.T) {
	t.Parallel()
	b, timers, sender, _ := newTestBatcher(t, 5*time.Minute)
	b.Enqueue(makeEvent("alice", "/doc", "assigned"))

	if got := len(sender.snapshot()); got != 0 {
		t.Fatalf("sent before window elapsed: %d calls", got)
	}
	if len(*timers) != 1 {
		t.Fatalf("want 1 pending timer, got %d", len(*timers))
	}
	(*timers)[0].trigger()

	calls := sender.snapshot()
	if len(calls) != 1 || calls[0].user != "alice" || len(calls[0].events) != 1 {
		t.Errorf("want one batch for alice with 1 event, got %+v", calls)
	}
}

func TestBatcher_SecondTriggerWithinWindow_ResetsTimerAndJoinsBatch(t *testing.T) {
	t.Parallel()
	b, timers, sender, _ := newTestBatcher(t, 5*time.Minute)
	b.Enqueue(makeEvent("alice", "/doc-a", "assigned"))
	b.Enqueue(makeEvent("alice", "/doc-b", "due_reminder"))

	// Two Enqueues: the first timer was Stop()'d, a second was
	// scheduled.
	if len(*timers) != 2 {
		t.Fatalf("want 2 timer attempts (one stopped, one live), got %d", len(*timers))
	}
	if !(*timers)[0].stopped {
		t.Errorf("first timer should have been stopped by the second Enqueue")
	}
	(*timers)[1].trigger()

	calls := sender.snapshot()
	if len(calls) != 1 {
		t.Fatalf("want 1 batch, got %d", len(calls))
	}
	if len(calls[0].events) != 2 {
		t.Errorf("want 2 events in the batch, got %d", len(calls[0].events))
	}
}

func TestBatcher_SamePageMultipleTriggers_DedupeToLatest(t *testing.T) {
	t.Parallel()
	b, timers, sender, _ := newTestBatcher(t, 5*time.Minute)
	b.Enqueue(makeEvent("alice", "/doc", "assigned"))
	b.Enqueue(makeEvent("alice", "/doc", "due_reminder"))
	b.Enqueue(makeEvent("alice", "/doc", "overdue"))
	(*timers)[len(*timers)-1].trigger()

	calls := sender.snapshot()
	if len(calls) != 1 || len(calls[0].events) != 1 {
		t.Fatalf("want 1 batch with 1 event (deduped by page), got %+v", calls)
	}
	if calls[0].events[0].Type != "overdue" {
		t.Errorf("deduped event type = %q, want overdue (the latest)", calls[0].events[0].Type)
	}
}

func TestBatcher_PerUserPageCapHoldsAcrossBatches(t *testing.T) {
	t.Parallel()
	b, timers, sender, cap := newTestBatcher(t, 1*time.Minute)
	b.Enqueue(makeEvent("alice", "/doc", "assigned"))
	(*timers)[len(*timers)-1].trigger()

	if len(sender.snapshot()) != 1 {
		t.Fatalf("first batch should have sent, got %d calls", len(sender.snapshot()))
	}

	// Second trigger for the same (user, page) within the cap: no new
	// batch should land.
	b.Enqueue(makeEvent("alice", "/doc", "due_reminder"))
	(*timers)[len(*timers)-1].trigger()

	if len(sender.snapshot()) != 1 {
		t.Errorf("second trigger inside 24h cap should be suppressed, got %d calls", len(sender.snapshot()))
	}

	// Simulate 24h passing by rewinding the cap's clock on the last
	// send record.
	cap.mu.Lock()
	for k := range cap.last {
		cap.last[k] = time.Now().Add(-25 * time.Hour)
	}
	cap.mu.Unlock()

	b.Enqueue(makeEvent("alice", "/doc", "due_reminder"))
	(*timers)[len(*timers)-1].trigger()

	if len(sender.snapshot()) != 2 {
		t.Errorf("trigger after cap expired should go through, got %d calls total", len(sender.snapshot()))
	}
}

func TestBatcher_DifferentUsersBatchIndependently(t *testing.T) {
	t.Parallel()
	b, timers, sender, _ := newTestBatcher(t, 1*time.Minute)
	b.Enqueue(makeEvent("alice", "/doc", "assigned"))
	b.Enqueue(makeEvent("bob", "/doc", "assigned"))
	if len(*timers) != 2 {
		t.Fatalf("want one timer per user, got %d", len(*timers))
	}
	(*timers)[0].trigger()
	(*timers)[1].trigger()

	calls := sender.snapshot()
	if len(calls) != 2 {
		t.Errorf("want one batch per user, got %d", len(calls))
	}
	users := map[string]bool{}
	for _, c := range calls {
		users[c.user] = true
	}
	if !users["alice"] || !users["bob"] {
		t.Errorf("both users should have received a batch, got %v", users)
	}
}

func TestBatcher_ZeroWindow_SendsImmediatelyWithCap(t *testing.T) {
	t.Parallel()
	b, timers, sender, _ := newTestBatcher(t, 0)
	b.Enqueue(makeEvent("alice", "/doc", "assigned"))

	// Zero window = send straight through, no timer scheduled.
	if len(*timers) != 0 {
		t.Errorf("zero window should not schedule a timer, got %d", len(*timers))
	}
	if len(sender.snapshot()) != 1 {
		t.Fatalf("want 1 call, got %d", len(sender.snapshot()))
	}

	// Second trigger inside the 24h cap is still suppressed even
	// without debouncing — the cap is independent of the window.
	b.Enqueue(makeEvent("alice", "/doc", "assigned"))
	if len(sender.snapshot()) != 1 {
		t.Errorf("second trigger inside cap should be suppressed, got %d", len(sender.snapshot()))
	}
}

func TestBatcher_Shutdown_FlushesPending(t *testing.T) {
	t.Parallel()
	b, timers, sender, _ := newTestBatcher(t, 5*time.Minute)
	b.Enqueue(makeEvent("alice", "/doc-a", "assigned"))
	b.Enqueue(makeEvent("alice", "/doc-b", "due_reminder"))

	if len(sender.snapshot()) != 0 {
		t.Fatalf("want no pre-shutdown sends, got %d", len(sender.snapshot()))
	}

	b.Shutdown()

	calls := sender.snapshot()
	if len(calls) != 1 || len(calls[0].events) != 2 {
		t.Errorf("Shutdown should flush the pending batch, got %+v", calls)
	}

	// Any surviving timer should have been Stop()'d so it can't fire
	// a duplicate after shutdown.
	for i, tim := range *timers {
		if tim.fired {
			t.Errorf("timer %d fired after shutdown — would double-send", i)
		}
	}

	// Post-shutdown Enqueue is a no-op.
	b.Enqueue(makeEvent("alice", "/doc-c", "overdue"))
	if len(sender.snapshot()) != 1 {
		t.Errorf("post-shutdown Enqueue should be a no-op, got %d calls", len(sender.snapshot()))
	}
}

func TestBatcher_EventWithoutUser_IsDropped(t *testing.T) {
	t.Parallel()
	b, timers, sender, _ := newTestBatcher(t, 1*time.Minute)
	b.Enqueue(NotifyEvent{Type: "assigned", Task: &Task{ID: "x", SourcePage: "/p"}})
	if len(*timers) != 0 || len(sender.snapshot()) != 0 {
		t.Errorf("event with no UserID/Recipient should be dropped silently")
	}
}
