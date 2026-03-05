package todo

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/smtp"
	"strings"
	"text/template"
	"time"

	"gowiki/backend/internal/config"
)

// Dispatcher sends notifications via email and webhooks.
type Dispatcher struct {
	notifyCfg config.TodoNotifyConfig
	siteTitle string
}

// NewDispatcher creates a new notification dispatcher.
func NewDispatcher(notifyCfg config.TodoNotifyConfig, siteTitle string) *Dispatcher {
	return &Dispatcher{
		notifyCfg: notifyCfg,
		siteTitle: siteTitle,
	}
}

// Notify sends a notification for a task event.
// If any webhook is enabled, email is suppressed.
func (d *Dispatcher) Notify(event NotifyEvent) {
	hasWebhook := false
	for _, wh := range d.notifyCfg.Webhooks {
		if wh.Enabled {
			hasWebhook = true
			d.sendWebhook(wh, event)
		}
	}

	if !hasWebhook && d.notifyCfg.Email.Enabled {
		d.sendEmail(event)
	}
}

func (d *Dispatcher) sendEmail(event NotifyEvent) {
	cfg := d.notifyCfg.Email
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
	var auth smtp.Auth
	if cfg.SMTPUser != "" {
		auth = smtp.PlainAuth("", cfg.SMTPUser, cfg.SMTPPass, cfg.SMTPHost)
	}

	if err := smtp.SendMail(addr, auth, cfg.From, []string{event.Recipient}, []byte(msg)); err != nil {
		log.Printf("todo notify: email send failed to %s: %v", event.Recipient, err)
	}
}

func (d *Dispatcher) renderEmailTemplate(event NotifyEvent) (string, string) {
	task := event.Task
	if task == nil {
		return "", ""
	}

	data := map[string]string{
		"SiteTitle": d.siteTitle,
		"Title":     task.Title,
		"Assignee":  task.Assignee.Target,
		"DueDate":   task.DueDate,
		"Priority":  string(task.Priority),
		"Page":      task.SourcePage,
	}

	switch event.Type {
	case "assigned":
		return fmt.Sprintf("[%s] Task assigned: %s", d.siteTitle, task.Title),
			renderTemplate(assignedTmpl, data)
	case "due_reminder":
		return fmt.Sprintf("[%s] Task due soon: %s", d.siteTitle, task.Title),
			renderTemplate(dueReminderTmpl, data)
	case "overdue":
		return fmt.Sprintf("[%s] Task overdue: %s", d.siteTitle, task.Title),
			renderTemplate(overdueTmpl, data)
	case "completed_all":
		return fmt.Sprintf("[%s] Task completed: %s", d.siteTitle, task.Title),
			renderTemplate(completedAllTmpl, data)
	case "recurrence_spawned":
		return fmt.Sprintf("[%s] Recurring task created: %s", d.siteTitle, task.Title),
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
Priority: {{.Priority}}
Due: {{.DueDate}}
Page: {{.Page}}
`

const dueReminderTmpl = `A task assigned to you is due soon on {{.SiteTitle}}.

Task: {{.Title}}
Due: {{.DueDate}}
Page: {{.Page}}
`

const overdueTmpl = `A task assigned to you is overdue on {{.SiteTitle}}.

Task: {{.Title}}
Due: {{.DueDate}}
Page: {{.Page}}
`

const completedAllTmpl = `A task has been completed on {{.SiteTitle}}.

Task: {{.Title}}
Page: {{.Page}}
`

const recurrenceSpawnedTmpl = `A recurring task has been created on {{.SiteTitle}}.

Task: {{.Title}}
Due: {{.DueDate}}
Page: {{.Page}}
`
