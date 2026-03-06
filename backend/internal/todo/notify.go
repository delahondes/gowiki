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
type Dispatcher struct {
	configReader  ConfigReader
	emailResolver EmailResolver
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
	// Resolve username → email if an email resolver is available.
	if event.Recipient != "" && d.emailResolver != nil && !strings.Contains(event.Recipient, "@") {
		email := d.emailResolver(event.Recipient)
		if email != "" {
			event.Recipient = email
		}
	}

	notifyCfg, _, _ := d.getConfig()
	hasWebhook := false
	for _, wh := range notifyCfg.Webhooks {
		if wh.Enabled {
			hasWebhook = true
			d.sendWebhook(wh, event)
		}
	}

	if !hasWebhook && notifyCfg.Email.Enabled {
		d.sendEmail(event)
	}
}

func (d *Dispatcher) sendEmail(event NotifyEvent) {
	notifyCfg, _, _ := d.getConfig()
	cfg := notifyCfg.Email
	if cfg.SMTPHost == "" || cfg.From == "" || event.Recipient == "" {
		return
	}

	subject, body := d.renderEmailTemplate(event)
	if subject == "" {
		return
	}

	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n%s",
		cfg.From, event.Recipient, subject, body)

	addr := fmt.Sprintf("%s:%d", cfg.SMTPHost, cfg.SMTPPort)

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
	if err := client.Rcpt(event.Recipient); err != nil {
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
