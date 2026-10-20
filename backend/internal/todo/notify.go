package todo

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/smtp"
	"strconv"
	"strings"
	"text/template"
	"time"

	"gowiki/backend/internal/config"
)

// ConfigReader provides live access to the current configuration.
type ConfigReader interface {
	Get() config.Config
}

// EmailResolver maps a username to an email address.
// Returns "" if the user has no email configured.
type EmailResolver func(username string) string

// Dispatcher sends notifications via email and webhooks.
// It reads configuration live from the config store so that admin UI
// changes take effect immediately without a restart.
//
// Email delivery is routed through a NotificationBatcher that
// debounces + groups per-user and enforces a 24h cap per (user,
// page). Webhook delivery is NOT batched — webhooks are machine-to-
// machine channels where the downstream system typically wants every
// event in real time; batching there would hide the signal the
// webhook consumer is subscribed to. The 24h cap also does not apply
// to webhooks for the same reason.
type Dispatcher struct {
	configReader  ConfigReader
	emailResolver EmailResolver
	batcher       *NotificationBatcher
	// Fallback for when no config reader is available (e.g. tests).
	notifyCfg *config.TodoNotifyConfig
	siteTitle string
}

// NewDispatcher creates a dispatcher that reads config live from the store.
func NewDispatcher(configReader ConfigReader, emailResolver EmailResolver) *Dispatcher {
	return &Dispatcher{configReader: configReader, emailResolver: emailResolver}
}

// NewDispatcherStatic creates a dispatcher with a fixed config snapshot (for tests).
func NewDispatcherStatic(notifyCfg config.TodoNotifyConfig, siteTitle string) *Dispatcher {
	return &Dispatcher{notifyCfg: &notifyCfg, siteTitle: siteTitle}
}

// AttachBatcher wires up the throttling + batching layer. Dispatcher
// stays usable without a batcher (every email ships immediately,
// no cap) so test helpers and startup-time code can call Notify
// before the database is open.
func (d *Dispatcher) AttachBatcher(b *NotificationBatcher) {
	d.batcher = b
}

// Shutdown flushes the batcher synchronously so a graceful restart
// doesn't drop queued notifications. Safe to call even with no
// attached batcher.
func (d *Dispatcher) Shutdown() {
	if d.batcher != nil {
		d.batcher.Shutdown()
	}
}

func (d *Dispatcher) getConfig() (config.TodoNotifyConfig, string, string) {
	if d.configReader != nil {
		cfg := d.configReader.Get()
		return cfg.Todo.Notify, cfg.Site.Title, cfg.Site.BaseURL
	}
	if d.notifyCfg != nil {
		return *d.notifyCfg, d.siteTitle, ""
	}
	return config.TodoNotifyConfig{}, "", ""
}

// Notify sends a notification for a task event.
// If any webhook is enabled, email is suppressed.
func (d *Dispatcher) Notify(event NotifyEvent) {
	notifyCfg, _, _ := d.getConfig()
	hasWebhook := false
	for _, wh := range notifyCfg.Webhooks {
		if wh.Enabled {
			hasWebhook = true
			d.sendWebhook(wh, event)
		}
	}
	if hasWebhook {
		return
	}

	if !notifyCfg.Email.Enabled {
		return
	}

	// Preserve the username (UserID) for batching + 24h cap keying.
	// Recipient gets resolved to an email either here (no batcher) or
	// at send time inside SendBatch (batcher path), so by the time we
	// touch SMTP the recipient is always an address.
	if d.batcher != nil {
		d.batcher.Enqueue(event)
		return
	}
	if event.Recipient != "" && d.emailResolver != nil && !strings.Contains(event.Recipient, "@") {
		email := d.emailResolver(event.Recipient)
		if email != "" {
			event.Recipient = email
		}
	}
	d.sendEmail(event)
}

// SendBatch implements NotificationSender. Called by the batcher
// when a user's debounce window elapses. events are already
// cap-filtered and de-duped by page_key; the dispatcher's job is to
// render them into one email and ship it.
func (d *Dispatcher) SendBatch(user string, events []NotifyEvent) {
	if len(events) == 0 {
		return
	}
	recipient := user
	if d.emailResolver != nil && !strings.Contains(recipient, "@") {
		if email := d.emailResolver(recipient); email != "" {
			recipient = email
		}
	}
	if !strings.Contains(recipient, "@") {
		// Nothing we can send to. Drop silently; the cap record is
		// updated by the batcher regardless, so a user with a missing
		// email address won't retry-storm.
		return
	}
	notifyCfg, _, _ := d.getConfig()
	if !notifyCfg.Email.Enabled {
		return
	}
	// Point every event at the resolved address before rendering so
	// the single-event path (one call to sendEmail) still works.
	if len(events) == 1 {
		ev := events[0]
		ev.Recipient = recipient
		d.sendEmail(ev)
		return
	}
	d.sendBatchedEmail(recipient, events)
}

func (d *Dispatcher) sendEmail(event NotifyEvent) {
	subject, body := d.renderEmailTemplate(event)
	if subject == "" {
		return
	}
	d.shipSMTP(event.Recipient, subject, body)
}

// sendBatchedEmail collapses several events for one recipient into a
// single message. Called by the batcher when its debounce window
// elapses with more than one event queued for the user; single-event
// batches go through the regular sendEmail path to preserve the
// pre-existing one-task email shape.
func (d *Dispatcher) sendBatchedEmail(recipient string, events []NotifyEvent) {
	subject, body := d.renderBatchedEmail(events)
	if subject == "" {
		return
	}
	d.shipSMTP(recipient, subject, body)
}

// shipSMTP is the SMTP transport common to both single and batched
// emails. Rendering happens upstream; this function knows nothing
// about event types, only addresses and bytes.
func (d *Dispatcher) shipSMTP(recipient, subject, body string) {
	notifyCfg, _, _ := d.getConfig()
	cfg := notifyCfg.Email
	if cfg.SMTPHost == "" || cfg.From == "" || recipient == "" {
		return
	}

	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n%s",
		cfg.From, recipient, subject, body)

	addr := net.JoinHostPort(cfg.SMTPHost, strconv.Itoa(cfg.SMTPPort))

	conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		log.Printf("todo notify: SMTP connect failed: %v", err)
		return
	}

	client, err := smtp.NewClient(conn, cfg.SMTPHost)
	if err != nil {
		conn.Close()
		log.Printf("todo notify: SMTP client failed: %v", err)
		return
	}
	defer client.Close()

	// STARTTLS if supported.
	if ok, _ := client.Extension("STARTTLS"); ok {
		tlsConfig := &tls.Config{ServerName: cfg.SMTPHost}
		if err := client.StartTLS(tlsConfig); err != nil {
			log.Printf("todo notify: STARTTLS failed: %v", err)
			return
		}
	}

	// Authenticate if credentials are configured.
	if cfg.SMTPUser != "" {
		// Try LOGIN auth first (required by Office 365), fall back to PLAIN.
		auth := &loginAuth{user: cfg.SMTPUser, pass: cfg.SMTPPass, host: cfg.SMTPHost}
		if err := client.Auth(auth); err != nil {
			// Fall back to PLAIN auth.
			plainAuth := smtp.PlainAuth("", cfg.SMTPUser, cfg.SMTPPass, cfg.SMTPHost)
			if err := client.Auth(plainAuth); err != nil {
				log.Printf("todo notify: SMTP auth failed: %v", err)
				return
			}
		}
	}

	if err := client.Mail(cfg.From); err != nil {
		log.Printf("todo notify: SMTP MAIL FROM failed: %v", err)
		return
	}
	if err := client.Rcpt(recipient); err != nil {
		log.Printf("todo notify: SMTP RCPT TO failed: %v", err)
		return
	}

	wc, err := client.Data()
	if err != nil {
		log.Printf("todo notify: SMTP DATA failed: %v", err)
		return
	}
	if _, err := wc.Write([]byte(msg)); err != nil {
		log.Printf("todo notify: SMTP write failed: %v", err)
		wc.Close()
		return
	}
	if err := wc.Close(); err != nil {
		log.Printf("todo notify: SMTP close data failed: %v", err)
		return
	}

	client.Quit()
}

// loginAuth implements smtp.Auth for the LOGIN mechanism (required by Office 365).
type loginAuth struct {
	user, pass, host string
}

func (a *loginAuth) Start(server *smtp.ServerInfo) (string, []byte, error) {
	return "LOGIN", []byte(a.user), nil
}

func (a *loginAuth) Next(fromServer []byte, more bool) ([]byte, error) {
	if more {
		prompt := strings.TrimSpace(string(fromServer))
		switch strings.ToLower(prompt) {
		case "username:":
			return []byte(a.user), nil
		case "password:":
			return []byte(a.pass), nil
		default:
			return nil, fmt.Errorf("unexpected LOGIN prompt: %q", prompt)
		}
	}
	return nil, nil
}

func (d *Dispatcher) renderEmailTemplate(event NotifyEvent) (string, string) {
	task := event.Task
	if task == nil {
		return "", ""
	}

	_, siteTitle, baseURL := d.getConfig()
	// Strip trailing slash from base URL for clean concatenation.
	baseURL = strings.TrimRight(baseURL, "/")

	pageURL := task.SourcePage
	if baseURL != "" && task.SourcePage != "" {
		pageURL = baseURL + task.SourcePage
	}

	actionLabel := ""
	actionURL := ""
	if task.WikiAction.Type != "" && task.WikiAction.Page != "" {
		switch task.WikiAction.Type {
		case "read":
			actionLabel = "Read " + task.WikiAction.Page
			if baseURL != "" {
				actionURL = baseURL + task.WikiAction.Page
			} else {
				actionURL = task.WikiAction.Page
			}
		case "edit":
			actionLabel = "Edit " + task.WikiAction.Page
			if baseURL != "" {
				actionURL = baseURL + task.WikiAction.Page + "?edit=1"
			} else {
				actionURL = task.WikiAction.Page + "?edit=1"
			}
		case "create":
			actionLabel = "Create " + task.WikiAction.Page
			if baseURL != "" {
				actionURL = baseURL + task.WikiAction.Page + "?edit=1"
			} else {
				actionURL = task.WikiAction.Page + "?edit=1"
			}
		}
	}

	data := map[string]string{
		"SiteTitle":   siteTitle,
		"Title":       task.Title,
		"Description": task.Description,
		"Assignee":    task.Assignee.Target,
		"DueDate":     task.DueDate,
		"Priority":    string(task.Priority),
		"Page":        task.SourcePage,
		"PageURL":     pageURL,
		"ActionLabel": actionLabel,
		"ActionURL":   actionURL,
	}

	switch event.Type {
	case "assigned":
		return fmt.Sprintf("[%s] Task assigned: %s", siteTitle, task.Title),
			renderTemplate(assignedTmpl, data)
	case "due_reminder":
		return fmt.Sprintf("[%s] Task due soon: %s", siteTitle, task.Title),
			renderTemplate(dueReminderTmpl, data)
	case "overdue":
		return fmt.Sprintf("[%s] Task overdue: %s", siteTitle, task.Title),
			renderTemplate(overdueTmpl, data)
	case "completed_all":
		return fmt.Sprintf("[%s] Task completed: %s", siteTitle, task.Title),
			renderTemplate(completedAllTmpl, data)
	case "recurrence_spawned":
		return fmt.Sprintf("[%s] Recurring task created: %s", siteTitle, task.Title),
			renderTemplate(recurrenceSpawnedTmpl, data)
	}
	return "", ""
}

// renderBatchedEmail collapses multiple events (already cap-filtered
// and de-duped by page_key upstream) into one email. The subject
// counts the events; the body lists one line per event with its type,
// title, and link.
func (d *Dispatcher) renderBatchedEmail(events []NotifyEvent) (string, string) {
	if len(events) == 0 {
		return "", ""
	}
	_, siteTitle, baseURL := d.getConfig()
	baseURL = strings.TrimRight(baseURL, "/")

	var sb strings.Builder
	fmt.Fprintf(&sb, "You have %d task updates on %s:\n\n", len(events), siteTitle)
	for _, ev := range events {
		if ev.Task == nil {
			continue
		}
		label := batchedEventLabel(ev.Type)
		pageURL := ev.Task.SourcePage
		if baseURL != "" && ev.Task.SourcePage != "" {
			pageURL = baseURL + ev.Task.SourcePage
		}
		fmt.Fprintf(&sb, "- [%s] %s", label, ev.Task.Title)
		if ev.Task.DueDate != "" {
			fmt.Fprintf(&sb, " (due %s)", ev.Task.DueDate)
		}
		if pageURL != "" {
			fmt.Fprintf(&sb, "\n  %s", pageURL)
		}
		sb.WriteString("\n")
	}
	sb.WriteString("\n")
	sb.WriteString("This digest batches updates that landed in a short window to keep your inbox quiet. " +
		"You will receive at most one email per document per day.\n")

	subject := fmt.Sprintf("[%s] %d task updates", siteTitle, len(events))
	return subject, sb.String()
}

// batchedEventLabel maps a NotifyEvent.Type to a short human label
// for the digest email's bullet points.
func batchedEventLabel(t string) string {
	switch t {
	case "assigned":
		return "Assigned"
	case "due_reminder":
		return "Due soon"
	case "overdue":
		return "Overdue"
	case "completed_all":
		return "Completed"
	case "recurrence_spawned":
		return "New instance"
	}
	return t
}

func (d *Dispatcher) sendWebhook(wh config.TodoWebhookConfig, event NotifyEvent) {
	payload, err := d.buildWebhookPayload(wh, event)
	if err != nil {
		log.Printf("todo notify: webhook %s payload build failed: %v", wh.Name, err)
		return
	}

	contentType := wh.ContentType
	if contentType == "" {
		contentType = "application/json"
	}

	req, err := http.NewRequest("POST", wh.URL, bytes.NewReader(payload))
	if err != nil {
		log.Printf("todo notify: webhook %s request build failed: %v", wh.Name, err)
		return
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("User-Agent", "Gowiki-Todo/1.0")

	if wh.HMACSecret != "" {
		mac := hmac.New(sha256.New, []byte(wh.HMACSecret))
		mac.Write(payload)
		sig := hex.EncodeToString(mac.Sum(nil))
		req.Header.Set("X-Gowiki-Signature", "sha256="+sig)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("todo notify: webhook %s send failed: %v", wh.Name, err)
		return
	}
	resp.Body.Close()
	if resp.StatusCode >= 400 {
		log.Printf("todo notify: webhook %s returned %d", wh.Name, resp.StatusCode)
	}
}

func (d *Dispatcher) buildWebhookPayload(wh config.TodoWebhookConfig, event NotifyEvent) ([]byte, error) {
	if wh.PayloadTmpl != "" {
		tmpl, err := template.New("payload").Parse(wh.PayloadTmpl)
		if err != nil {
			return nil, fmt.Errorf("parse payload template: %w", err)
		}
		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, event); err != nil {
			return nil, fmt.Errorf("execute payload template: %w", err)
		}
		return buf.Bytes(), nil
	}

	return json.Marshal(map[string]any{
		"event": event.Type,
		"task":  event.Task,
	})
}

func renderTemplate(tmpl string, data map[string]string) string {
	t, err := template.New("email").Parse(tmpl)
	if err != nil {
		return tmpl
	}
	var buf strings.Builder
	if err := t.Execute(&buf, data); err != nil {
		return tmpl
	}
	return buf.String()
}

// Email templates (embedded as constants — no go:embed needed for plain text).

const assignedTmpl = `You have been assigned a task on {{.SiteTitle}}.

Task: {{.Title}}
{{if .Description}}Description: {{.Description}}
{{end}}Priority: {{.Priority}}
Due: {{.DueDate}}
Page: {{.PageURL}}
{{if .ActionLabel}}Action: {{.ActionLabel}} — {{.ActionURL}}
{{end}}`

const dueReminderTmpl = `A task assigned to you is due soon on {{.SiteTitle}}.

Task: {{.Title}}
{{if .Description}}Description: {{.Description}}
{{end}}Due: {{.DueDate}}
Page: {{.PageURL}}
{{if .ActionLabel}}Action: {{.ActionLabel}} — {{.ActionURL}}
{{end}}`

const overdueTmpl = `A task assigned to you is overdue on {{.SiteTitle}}.

Task: {{.Title}}
{{if .Description}}Description: {{.Description}}
{{end}}Due: {{.DueDate}}
Page: {{.PageURL}}
{{if .ActionLabel}}Action: {{.ActionLabel}} — {{.ActionURL}}
{{end}}`

const completedAllTmpl = `A task has been completed on {{.SiteTitle}}.

Task: {{.Title}}
{{if .Description}}Description: {{.Description}}
{{end}}Page: {{.PageURL}}
`

const recurrenceSpawnedTmpl = `A recurring task has been created on {{.SiteTitle}}.

Task: {{.Title}}
{{if .Description}}Description: {{.Description}}
{{end}}Due: {{.DueDate}}
Page: {{.PageURL}}
{{if .ActionLabel}}Action: {{.ActionLabel}} — {{.ActionURL}}
{{end}}`
