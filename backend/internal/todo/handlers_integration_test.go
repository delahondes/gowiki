//go:build integration

package todo

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"gowiki/backend/internal/auth"
)

// The handlers are mounted under /api/plugin/todo/v1 in the real server;
// here we mount the same router under / for direct testing. The extract
// username closure reads from a well-known header so tests can toggle
// "authenticated as alice" vs unauth in one line.

func newHandlerHarness(t *testing.T) (chi.Router, *TodoService, *auth.UserStore, *auth.GroupStore) {
	t.Helper()
	pool := newTestPool(t)
	store := NewTodoStore(pool)
	hub := NewHub()
	svc := NewService(store, hub, nil)

	meta := t.TempDir()
	userStore, err := auth.NewUserStore(meta)
	if err != nil {
		t.Fatalf("NewUserStore: %v", err)
	}
	groupStore, err := auth.NewGroupStore(meta)
	if err != nil {
		t.Fatalf("NewGroupStore: %v", err)
	}
	// Seed a couple of users. NewGroupStore auto-provisions the "editors"
	// group at first use, so we only create it if it isn't already there.
	if err := userStore.Create(auth.User{Username: "alice", Email: "a@x", Groups: []string{"editors"}}, "pw"); err != nil {
		t.Fatalf("create alice: %v", err)
	}
	if err := userStore.Create(auth.User{Username: "bob", Email: "b@x"}, "pw"); err != nil {
		t.Fatalf("create bob: %v", err)
	}
	haveEditors := false
	for _, g := range groupStore.List() {
		if g.Name == "editors" {
			haveEditors = true
			break
		}
	}
	if !haveEditors {
		if err := groupStore.Create(auth.Group{Name: "editors"}); err != nil {
			t.Fatalf("create editors: %v", err)
		}
	}

	r := chi.NewRouter()
	// Extractor: X-Test-User header is our stand-in for "authenticated as".
	extract := UsernameExtractor(func(r *http.Request) string { return r.Header.Get("X-Test-User") })
	RegisterRoutes(r, svc, userStore, groupStore, extract, nil, nil)

	return r, svc, userStore, groupStore
}

func do(t *testing.T, r chi.Router, method, path, user string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf *bytes.Buffer
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		buf = bytes.NewBuffer(b)
	} else {
		buf = bytes.NewBuffer(nil)
	}
	req := httptest.NewRequest(method, path, buf)
	if user != "" {
		req.Header.Set("X-Test-User", user)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func decode(t *testing.T, rec *httptest.ResponseRecorder, out any) {
	t.Helper()
	if err := json.NewDecoder(rec.Body).Decode(out); err != nil {
		t.Fatalf("decode body: %v (body=%s)", err, rec.Body.String())
	}
}

func TestHandleCreate_HappyPath(t *testing.T) {
	t.Parallel()
	r, _, _, _ := newHandlerHarness(t)
	rec := do(t, r, "POST", "/tasks", "alice", CreateRequest{
		Title:    "Hello",
		Assignee: Assignee{Type: "user", Target: "alice"},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var task Task
	decode(t, rec, &task)
	if task.Title != "Hello" || task.CreatedBy != "alice" {
		t.Errorf("task = %+v", task)
	}
}

func TestHandleCreate_MissingTitle_400(t *testing.T) {
	t.Parallel()
	r, _, _, _ := newHandlerHarness(t)
	rec := do(t, r, "POST", "/tasks", "alice", CreateRequest{Title: ""})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestHandleCreate_InvalidJSON_400(t *testing.T) {
	t.Parallel()
	r, _, _, _ := newHandlerHarness(t)
	req := httptest.NewRequest("POST", "/tasks", strings.NewReader("not-json"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Test-User", "alice")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestHandleList_ReturnsAll(t *testing.T) {
	t.Parallel()
	r, svc, _, _ := newHandlerHarness(t)
	_, _ = svc.CreateTask(context.Background(), CreateRequest{Title: "A", Assignee: Assignee{Type: "user", Target: "alice"}})
	_, _ = svc.CreateTask(context.Background(), CreateRequest{Title: "B", Assignee: Assignee{Type: "user", Target: "bob"}})

	rec := do(t, r, "GET", "/tasks", "alice", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var resp struct {
		Tasks []*Task `json:"tasks"`
	}
	decode(t, rec, &resp)
	if len(resp.Tasks) != 2 {
		t.Errorf("got %d, want 2", len(resp.Tasks))
	}
}

func TestHandleList_FilterByAssignee(t *testing.T) {
	t.Parallel()
	r, svc, _, _ := newHandlerHarness(t)
	_, _ = svc.CreateTask(context.Background(), CreateRequest{Title: "A", Assignee: Assignee{Type: "user", Target: "alice"}})
	_, _ = svc.CreateTask(context.Background(), CreateRequest{Title: "B", Assignee: Assignee{Type: "user", Target: "bob"}})

	rec := do(t, r, "GET", "/tasks?assignee=alice", "alice", nil)
	var resp struct {
		Tasks []*Task `json:"tasks"`
	}
	decode(t, rec, &resp)
	if len(resp.Tasks) != 1 || resp.Tasks[0].Title != "A" {
		t.Errorf("filter failed: %+v", resp.Tasks)
	}
}

func TestHandleGet_And_Delete(t *testing.T) {
	t.Parallel()
	r, svc, _, _ := newHandlerHarness(t)
	task, _ := svc.CreateTask(context.Background(), CreateRequest{Title: "X", Assignee: Assignee{Type: "user", Target: "alice"}})

	rec := do(t, r, "GET", "/tasks/"+task.ID, "alice", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get status = %d, body=%s", rec.Code, rec.Body.String())
	}

	rec = do(t, r, "DELETE", "/tasks/"+task.ID, "alice", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status = %d", rec.Code)
	}

	rec = do(t, r, "GET", "/tasks/"+task.ID, "alice", nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("get-after-delete status = %d, want 404", rec.Code)
	}
}

func TestHandleGet_NotFound(t *testing.T) {
	t.Parallel()
	r, _, _, _ := newHandlerHarness(t)
	rec := do(t, r, "GET", "/tasks/does-not-exist", "alice", nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestHandlePatch_UpdatesFields(t *testing.T) {
	t.Parallel()
	r, svc, _, _ := newHandlerHarness(t)
	task, _ := svc.CreateTask(context.Background(), CreateRequest{Title: "Orig", Assignee: Assignee{Type: "user", Target: "alice"}})

	newTitle := "Renamed"
	rec := do(t, r, "PATCH", "/tasks/"+task.ID, "alice", Patch{Title: &newTitle})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var got Task
	decode(t, rec, &got)
	if got.Title != "Renamed" {
		t.Errorf("title = %q, want Renamed", got.Title)
	}
}

func TestHandleComplete_ThenReopen(t *testing.T) {
	t.Parallel()
	r, svc, _, _ := newHandlerHarness(t)
	task, _ := svc.CreateTask(context.Background(), CreateRequest{Title: "T", Assignee: Assignee{Type: "user", Target: "alice"}})

	rec := do(t, r, "POST", "/tasks/"+task.ID+"/complete", "alice", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("complete status = %d", rec.Code)
	}
	var completed Task
	decode(t, rec, &completed)
	if completed.Status != StatusDone {
		t.Errorf("status = %s, want done", completed.Status)
	}

	rec = do(t, r, "POST", "/tasks/"+task.ID+"/reopen", "alice", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("reopen status = %d", rec.Code)
	}
	var reopened Task
	decode(t, rec, &reopened)
	if reopened.Status != StatusOpen {
		t.Errorf("status = %s, want open", reopened.Status)
	}
}

func TestHandleMine_RequiresAuth(t *testing.T) {
	t.Parallel()
	r, _, _, _ := newHandlerHarness(t)
	rec := do(t, r, "GET", "/tasks/mine", "", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestHandleMine_ReturnsDirectAndGroupTasks(t *testing.T) {
	t.Parallel()
	r, svc, _, _ := newHandlerHarness(t)
	_, _ = svc.CreateTask(context.Background(), CreateRequest{Title: "Direct", Assignee: Assignee{Type: "user", Target: "alice"}})
	_, _ = svc.CreateTask(context.Background(), CreateRequest{Title: "Group", Assignee: Assignee{Type: "group", Target: "group:editors"}})
	_, _ = svc.CreateTask(context.Background(), CreateRequest{Title: "NotMine", Assignee: Assignee{Type: "user", Target: "bob"}})

	rec := do(t, r, "GET", "/tasks/mine", "alice", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Tasks []*Task `json:"tasks"`
	}
	decode(t, rec, &resp)
	titles := map[string]bool{}
	for _, t := range resp.Tasks {
		titles[t.Title] = true
	}
	if !titles["Direct"] || !titles["Group"] {
		t.Errorf("expected Direct + Group, got %v", titles)
	}
	if titles["NotMine"] {
		t.Errorf("should not include bob's task")
	}
}

func TestHandleByPage_UnauthOK_TasksReturned(t *testing.T) {
	t.Parallel()
	// handleByPage is registered on the read routes — no auth required.
	r, svc, _, _ := newHandlerHarness(t)
	_, _ = svc.CreateTask(context.Background(), CreateRequest{
		Title:      "on plans",
		SourcePage: "/plans/release",
		Assignee:   Assignee{Type: "user", Target: "alice"},
	})
	rec := do(t, r, "GET", "/tasks/page/plans/release", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Tasks []*Task `json:"tasks"`
	}
	decode(t, rec, &resp)
	if len(resp.Tasks) != 1 || resp.Tasks[0].Title != "on plans" {
		t.Errorf("tasks = %+v", resp.Tasks)
	}
}

func TestHandleByPage_MissingPath_400(t *testing.T) {
	t.Parallel()
	r, _, _, _ := newHandlerHarness(t)
	// Chi matches an empty * and reaches the handler; the handler's
	// missing-path guard returns 400.
	rec := do(t, r, "GET", "/tasks/page/", "", nil)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (handler's missing-path guard)", rec.Code)
	}
}

func TestHandleAckStatus_RequiresAuth(t *testing.T) {
	t.Parallel()
	r, _, _, _ := newHandlerHarness(t)
	rec := do(t, r, "GET", "/tasks/ack/some/page", "", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestHandleAcknowledge_Flow(t *testing.T) {
	t.Parallel()
	r, svc, _, _ := newHandlerHarness(t)
	task, _ := svc.CreateTask(context.Background(), CreateRequest{
		Title:      "Read the policy",
		Assignee:   Assignee{Type: "user", Target: "alice"},
		WikiAction: WikiAction{Type: "read", Page: "/policy"},
	})
	rec := do(t, r, "POST", "/tasks/"+task.ID+"/acknowledge", "alice", map[string]int64{"version": 7})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var got Task
	decode(t, rec, &got)
	if got.Status != StatusDone {
		t.Errorf("status = %s, want done", got.Status)
	}
}

func TestHandleDelete_NotFound(t *testing.T) {
	t.Parallel()
	r, _, _, _ := newHandlerHarness(t)
	rec := do(t, r, "DELETE", "/tasks/does-not-exist", "alice", nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}
