package todo

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"gowiki/backend/internal/auth"
)

// UsernameExtractor is a function that extracts the username from an HTTP request context.
// This avoids a circular import between the todo and api packages.
type UsernameExtractor func(r *http.Request) string

// RegisterRoutes mounts all todo API routes (read + write) on the given chi router.
// All routes are under /api/plugin/todo/v1/.
func RegisterRoutes(r chi.Router, svc *TodoService, userStore *auth.UserStore, groupStore *auth.GroupStore, extractUsername UsernameExtractor, pageChecker PageChecker, rfChecker ReviewflowChecker) {
	RegisterReadRoutes(r, svc, userStore, groupStore, extractUsername, pageChecker, rfChecker)
	RegisterWriteRoutes(r, svc, userStore, groupStore, extractUsername, pageChecker, rfChecker)
}

// RegisterReadRoutes mounts read-only todo endpoints (accessible without authentication).
func RegisterReadRoutes(r chi.Router, svc *TodoService, userStore *auth.UserStore, groupStore *auth.GroupStore, extractUsername UsernameExtractor, pageChecker PageChecker, rfChecker ReviewflowChecker) {
	h := &handlers{svc: svc, userStore: userStore, groupStore: groupStore, extractUsername: extractUsername, pageChecker: pageChecker, rfChecker: rfChecker}

	r.Get("/tasks/page/*", h.handleByPage)
}

// RegisterWriteRoutes mounts todo endpoints that require authentication.
func RegisterWriteRoutes(r chi.Router, svc *TodoService, userStore *auth.UserStore, groupStore *auth.GroupStore, extractUsername UsernameExtractor, pageChecker PageChecker, rfChecker ReviewflowChecker) {
	h := &handlers{svc: svc, userStore: userStore, groupStore: groupStore, extractUsername: extractUsername, pageChecker: pageChecker, rfChecker: rfChecker}

	r.Get("/tasks", h.handleList)
	r.Post("/tasks", h.handleCreate)
	r.Get("/tasks/mine", h.handleMine)
	r.Get("/tasks/ack/*", h.handleAckStatus)
	r.Get("/tasks/{id}", h.handleGet)
	r.Patch("/tasks/{id}", h.handlePatch)
	r.Delete("/tasks/{id}", h.handleDelete)
	r.Post("/tasks/{id}/complete", h.handleComplete)
	r.Post("/tasks/{id}/reopen", h.handleReopen)
	r.Post("/tasks/{id}/acknowledge", h.handleAcknowledge)
	r.Get("/stream", h.handleStream)
}

type handlers struct {
	svc             *TodoService
	userStore       *auth.UserStore
	groupStore      *auth.GroupStore
	extractUsername UsernameExtractor
	pageChecker     PageChecker
	rfChecker       ReviewflowChecker
}

func (h *handlers) handleList(w http.ResponseWriter, r *http.Request) {
	page := r.URL.Query().Get("page")
	if page != "" && !strings.HasPrefix(page, "/") {
		page = "/" + page
	}
	opts := ListOptions{
		Status:   Status(r.URL.Query().Get("status")),
		Assignee: r.URL.Query().Get("assignee"),
		Page:     page,
		Tag:      r.URL.Query().Get("tag"),
		Priority: Priority(r.URL.Query().Get("priority")),
		Cursor:   r.URL.Query().Get("cursor"),
		Limit:    parseInt(r.URL.Query().Get("limit")),
	}

	tasks, cursor, err := h.svc.Store().List(r.Context(), opts)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if tasks == nil {
		tasks = []*Task{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"tasks":  tasks,
		"cursor": cursor,
	})
}

func (h *handlers) handleCreate(w http.ResponseWriter, r *http.Request) {
	var req CreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}

	if req.Title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}

	req.CreatedBy = h.extractUsername(r)

	task, err := h.svc.CreateTask(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, task)
}

func (h *handlers) handleGet(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	task, err := h.svc.Store().Get(r.Context(), id)
	if err != nil {
		if strings.Contains(err.Error(), "no rows") {
			writeError(w, http.StatusNotFound, "task not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, task)
}

func (h *handlers) handlePatch(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var patch Patch
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}

	task, err := h.svc.Store().Update(r.Context(), id, patch)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeError(w, http.StatusNotFound, "task not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.svc.Hub().Publish(task.Assignee.Target, Event{Type: "task.updated", Task: task})
	writeJSON(w, http.StatusOK, task)
}

func (h *handlers) handleDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.svc.Store().Delete(r.Context(), id); err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeError(w, http.StatusNotFound, "task not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"deleted": id})
}

func (h *handlers) handleComplete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	userID := h.extractUsername(r)

	// Reject completion if the task's page has a pending reviewflow.
	if h.rfChecker != nil {
		task, err := h.svc.Store().Get(r.Context(), id)
		if err == nil && task.SourcePage != "" && !strings.Contains(task.Tags, "reviewflow") {
			if h.rfChecker.IsPageReviewPending(task.SourcePage) {
				writeError(w, http.StatusConflict, "task is inactive: page review is pending")
				return
			}
		}
	}

	var resolver GroupResolver
	if h.groupStore != nil && h.userStore != nil {
		resolver = &authGroupResolver{groupStore: h.groupStore, userStore: h.userStore}
	}

	task, err := h.svc.CompleteTask(r.Context(), id, userID, resolver)
	if err != nil {
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "no rows") {
			writeError(w, http.StatusNotFound, "task not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, task)
}

func (h *handlers) handleReopen(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	task, err := h.svc.ReopenTask(r.Context(), id)
	if err != nil {
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "no rows") {
			writeError(w, http.StatusNotFound, "task not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, task)
}

func (h *handlers) handleMine(w http.ResponseWriter, r *http.Request) {
	userID := h.extractUsername(r)
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	// Get user's groups.
	var groups []string
	if h.userStore != nil {
		user, err := h.userStore.Get(userID)
		if err == nil {
			groups = user.EffectiveGroups()
		}
	}

	tasks, err := h.svc.Store().ListMine(r.Context(), userID, groups)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if tasks == nil {
		tasks = []*Task{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"tasks": tasks})
}

func (h *handlers) handleByPage(w http.ResponseWriter, r *http.Request) {
	pagePath := strings.TrimSpace(chi.URLParam(r, "*"))
	if pagePath == "" {
		writeError(w, http.StatusBadRequest, "missing page path")
		return
	}
	// Chi URL params don't include leading "/"; canonical internal paths are "/"-prefixed.
	if !strings.HasPrefix(pagePath, "/") {
		pagePath = "/" + pagePath
	}

	tasks, err := h.svc.Store().ListForPage(r.Context(), pagePath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if tasks == nil {
		tasks = []*Task{}
	}
	h.addWarnings(tasks)
	writeJSON(w, http.StatusOK, map[string]any{"tasks": tasks})
}

func (h *handlers) handleAckStatus(w http.ResponseWriter, r *http.Request) {
	pagePath := strings.TrimSpace(chi.URLParam(r, "*"))
	if pagePath == "" {
		writeError(w, http.StatusBadRequest, "missing page path")
		return
	}
	if !strings.HasPrefix(pagePath, "/") {
		pagePath = "/" + pagePath
	}

	userID := h.extractUsername(r)
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	var groups []string
	if h.userStore != nil {
		user, err := h.userStore.Get(userID)
		if err == nil {
			groups = user.EffectiveGroups()
		}
	}

	acks, err := h.svc.Store().ListPendingAcks(r.Context(), pagePath, userID, groups)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if acks == nil {
		acks = []PendingAck{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"tasks": acks})
}

func (h *handlers) handleAcknowledge(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	userID := h.extractUsername(r)
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	var req struct {
		Version int64 `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}

	var resolver GroupResolver
	if h.groupStore != nil && h.userStore != nil {
		resolver = &authGroupResolver{groupStore: h.groupStore, userStore: h.userStore}
	}

	task, err := h.svc.AcknowledgeTask(r.Context(), id, userID, req.Version, resolver)
	if err != nil {
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "no rows") {
			writeError(w, http.StatusNotFound, "task not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, task)
}

func (h *handlers) handleStream(w http.ResponseWriter, r *http.Request) {
	userID := h.extractUsername(r)
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	h.svc.Hub().HandleStream(w, r, userID)
}

// addWarnings populates the Warnings and Inactive fields on tasks based on validation checks.
func (h *handlers) addWarnings(tasks []*Task) {
	// Cache reviewflow status per source page to avoid repeated checks.
	rfPendingCache := make(map[string]bool)

	for _, task := range tasks {
		// Check assignee exists.
		if task.Assignee.Target != "" && h.userStore != nil {
			if task.Assignee.Type == "user" {
				if _, err := h.userStore.Get(task.Assignee.Target); err != nil {
					task.Warnings = append(task.Warnings, fmt.Sprintf("Assignee user %q does not exist", task.Assignee.Target))
				}
			} else if task.Assignee.Type == "group" {
				if h.groupStore != nil {
					found := false
					for _, g := range h.groupStore.List() {
						if g.Name == task.Assignee.Target {
							found = true
							break
						}
					}
					if !found {
						task.Warnings = append(task.Warnings, fmt.Sprintf("Assignee group %q does not exist", task.Assignee.Target))
					}
				}
			}
		}

		// Check wiki action target page exists (read/edit only).
		if h.pageChecker != nil && task.WikiAction.Page != "" {
			if task.WikiAction.Type == "read" || task.WikiAction.Type == "edit" {
				if !h.pageChecker.PageExists(task.WikiAction.Page) {
					task.Warnings = append(task.Warnings, fmt.Sprintf("Action target page %q does not exist", task.WikiAction.Page))
				}
			}
		}

		// Mark tasks inactive if their source page has a pending reviewflow.
		// Exclude reviewflow's own tasks (tagged "reviewflow").
		if h.rfChecker != nil && task.SourcePage != "" && !strings.Contains(task.Tags, "reviewflow") {
			pending, ok := rfPendingCache[task.SourcePage]
			if !ok {
				pending = h.rfChecker.IsPageReviewPending(task.SourcePage)
				rfPendingCache[task.SourcePage] = pending
			}
			if pending {
				task.Inactive = true
			}
		}
	}
}

// --- Helpers ---

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}


// authGroupResolver resolves group membership using the auth stores.
type authGroupResolver struct {
	groupStore *auth.GroupStore
	userStore  *auth.UserStore
}

func (r *authGroupResolver) GroupMembers(groupName string) []string {
	users := r.userStore.List()
	var members []string
	for _, u := range users {
		for _, g := range u.EffectiveGroups() {
			if g == groupName {
				members = append(members, u.Username)
				break
			}
		}
	}
	return members
}
