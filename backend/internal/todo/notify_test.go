package todo

import (
	"encoding/json"
	"strings"
	"testing"

	"gowiki/backend/internal/config"
)

// The notify surface has an SMTP dial in sendEmail we don't exercise. The
// pure parts are renderEmailTemplate and buildWebhookPayload — both
// deterministic, unit-testable.

func newTestDispatcher() *Dispatcher {
	// The dispatcher's siteTitle field is populated by getConfig — see below.
	return NewDispatcherStatic(config.TodoNotifyConfig{}, "TestWiki")
}

func newTaskFixture() *Task {
	return &Task{
		ID:          "t1",
		Title:       "Ship the RC",
		Description: "Cut v1.0.0-rc.1 today",
		SourcePage:  "/plans/release",
		Priority:    PriorityHigh,
		DueDate:     "2026-12-01",
		Assignee:    Assignee{Type: "user", Target: "alice", Resolution: "any"},
		WikiAction:  WikiAction{Type: "read", Page: "/policies/release"},
	}
}

func TestRenderEmailTemplate_Assigned(t *testing.T) {
	t.Parallel()
	d := newTestDispatcher()
	subj, body := d.renderEmailTemplate(NotifyEvent{
		Type: "assigned",
		Task: newTaskFixture(),
	})
	if !strings.Contains(subj, "TestWiki") || !strings.Contains(subj, "Ship the RC") {
		t.Errorf("subject = %q; missing SiteTitle or Title", subj)
	}
	if !strings.Contains(body, "Priority: high") {
		t.Errorf("body should carry Priority; got:\n%s", body)
	}
	if !strings.Contains(body, "Due: 2026-12-01") {
		t.Errorf("body should carry due date")
	}
	if !strings.Contains(body, "Action: Read /policies/release") {
		t.Errorf("body should carry action label; got:\n%s", body)
	}
}

func TestRenderEmailTemplate_DueReminder(t *testing.T) {
	t.Parallel()
	d := newTestDispatcher()
	subj, body := d.renderEmailTemplate(NotifyEvent{Type: "due_reminder", Task: newTaskFixture()})
	if !strings.HasPrefix(subj, "[TestWiki] Task due soon:") {
		t.Errorf("subject = %q; missing due-soon prefix", subj)
	}
	if !strings.Contains(body, "is due soon") {
		t.Errorf("body should mention 'due soon'; got:\n%s", body)
	}
}

func TestRenderEmailTemplate_Overdue(t *testing.T) {
	t.Parallel()
	d := newTestDispatcher()
	subj, body := d.renderEmailTemplate(NotifyEvent{Type: "overdue", Task: newTaskFixture()})
	if !strings.Contains(subj, "overdue") {
		t.Errorf("subject = %q; missing 'overdue'", subj)
	}
	if !strings.Contains(body, "overdue") {
		t.Errorf("body should carry 'overdue'; got:\n%s", body)
	}
}

func TestRenderEmailTemplate_Unknown(t *testing.T) {
	t.Parallel()
	d := newTestDispatcher()
	subj, body := d.renderEmailTemplate(NotifyEvent{Type: "mystery", Task: newTaskFixture()})
	if subj != "" || body != "" {
		t.Errorf("unknown type should produce empty subject/body, got %q / %q", subj, body)
	}
}

func TestRenderEmailTemplate_NilTaskReturnsEmpty(t *testing.T) {
	t.Parallel()
	d := newTestDispatcher()
	subj, body := d.renderEmailTemplate(NotifyEvent{Type: "assigned", Task: nil})
	if subj != "" || body != "" {
		t.Errorf("nil task should short-circuit to empty; got %q / %q", subj, body)
	}
}

func TestBuildWebhookPayload_DefaultJSON(t *testing.T) {
	t.Parallel()
	d := newTestDispatcher()
	wh := config.TodoWebhookConfig{Name: "slack", Enabled: true, URL: "https://example.invalid/hook"}
	got, err := d.buildWebhookPayload(wh, NotifyEvent{Type: "assigned", Task: newTaskFixture()})
	if err != nil {
		t.Fatalf("build payload: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(got, &out); err != nil {
		t.Fatalf("default payload should be valid JSON: %v", err)
	}
	if out["event"] != "assigned" {
		t.Errorf("event = %v, want assigned", out["event"])
	}
	if _, ok := out["task"]; !ok {
		t.Errorf("payload should embed the task object")
	}
}

func TestBuildWebhookPayload_CustomTemplate(t *testing.T) {
	t.Parallel()
	d := newTestDispatcher()
	wh := config.TodoWebhookConfig{
		Name:        "slack",
		Enabled:     true,
		URL:         "https://example.invalid/hook",
		PayloadTmpl: `{"text":"{{.Type}}: {{.Task.Title}}"}`,
	}
	got, err := d.buildWebhookPayload(wh, NotifyEvent{Type: "assigned", Task: newTaskFixture()})
	if err != nil {
		t.Fatalf("build custom payload: %v", err)
	}
	if !strings.Contains(string(got), `"assigned: Ship the RC"`) {
		t.Errorf("custom payload = %s", got)
	}
}

func TestBuildWebhookPayload_InvalidTemplate(t *testing.T) {
	t.Parallel()
	d := newTestDispatcher()
	wh := config.TodoWebhookConfig{PayloadTmpl: "{{ not a valid template"}
	_, err := d.buildWebhookPayload(wh, NotifyEvent{Type: "assigned", Task: newTaskFixture()})
	if err == nil {
		t.Errorf("expected error on malformed template")
	}
}

func TestRenderTemplate_HandlesMalformed(t *testing.T) {
	t.Parallel()
	// The renderTemplate helper falls back to the raw template on parse error.
	out := renderTemplate("{{ this is broken", map[string]string{"X": "y"})
	if out != "{{ this is broken" {
		t.Errorf("malformed template should fall through; got %q", out)
	}
}
