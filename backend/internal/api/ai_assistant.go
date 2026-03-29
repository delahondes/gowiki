package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"gowiki/backend/internal/aiassistant"
)

// requireAIAssistantAccess checks that:
// 1. The AI assistant is enabled in config
// 2. The user belongs to an allowed group (if allowed_groups is set)
func (s *Server) requireAIAssistantAccess(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg := s.configStore.Get()
		if !cfg.AIAssistant.Enabled {
			writeError(w, http.StatusNotFound, "AI assistant is not enabled")
			return
		}

		username := UsernameFromContext(r.Context())
		if username == "" {
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}

		// If allowed_groups is set, check membership.
		if len(cfg.AIAssistant.AllowedGroups) > 0 {
			groups := s.effectiveGroups(username)
			if !hasAnyGroup(groups, cfg.AIAssistant.AllowedGroups) {
				writeError(w, http.StatusForbidden, "AI assistant access denied")
				return
			}
		}

		next.ServeHTTP(w, r)
	})
}

func hasAnyGroup(userGroups, allowedGroups []string) bool {
	allowed := make(map[string]bool, len(allowedGroups))
	for _, g := range allowedGroups {
		allowed[g] = true
	}
	for _, g := range userGroups {
		if allowed[g] {
			return true
		}
	}
	return false
}

// handleAIChat handles POST /api/ai/assistant/chat
func (s *Server) handleAIChat(w http.ResponseWriter, r *http.Request) {
	if s.aiProvider == nil {
		writeError(w, http.StatusServiceUnavailable, "AI provider not configured")
		return
	}

	var req struct {
		PagePath string `json:"page_path"`
		Message  string `json:"message"`
		Mode     string `json:"mode"` // "action" or "review"
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Message == "" {
		writeError(w, http.StatusBadRequest, "message is required")
		return
	}
	if req.Mode == "" {
		req.Mode = "action"
	}
	if req.Mode != "action" && req.Mode != "review" {
		writeError(w, http.StatusBadRequest, "mode must be 'action' or 'review'")
		return
	}

	username := UsernameFromContext(r.Context())
	cfg := s.configStore.Get()

	// Read page content for context (from draft if available, otherwise published).
	var pageContent string
	if req.PagePath != "" {
		// Check @ai ACL permission.
		if s.aclStore != nil && !s.aclStore.CheckPermission("@ai", nil, req.PagePath, "view") {
			writeError(w, http.StatusForbidden, "AI is not allowed to access this page")
			return
		}
		// Also check user's own permission.
		groups := s.effectiveGroups(username)
		if s.aclStore != nil && !s.aclStore.CheckPermission(username, groups, req.PagePath, "view") {
			writeError(w, http.StatusForbidden, "access denied")
			return
		}

		// Try draft first, then published.
		if s.draftManager != nil {
			if draft, err := s.draftManager.ReadDraft(req.PagePath, username); err == nil && draft != "" {
				pageContent = draft
			}
		}
		if pageContent == "" {
			if page, err := s.store.Get(req.PagePath); err == nil {
				pageContent = page.Markdown
			}
		}
	}

	// Build system prompt.
	systemPrompt := s.buildAISystemPrompt(req.Mode, pageContent, req.PagePath)

	// Build LLM request.
	chatReq := aiassistant.ChatRequest{
		SystemPrompt: systemPrompt,
		Messages: []aiassistant.Message{
			{Role: "user", Content: req.Message},
		},
		MaxTokens: cfg.AIAssistant.MaxTokens,
		Model:     cfg.AIAssistant.Model,
	}

	// Call the provider.
	eventCh, err := s.aiProvider.Chat(r.Context(), chatReq)
	if err != nil {
		writeError(w, http.StatusBadGateway, fmt.Sprintf("AI provider error: %v", err))
		return
	}

	// Stream SSE response.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	for event := range eventCh {
		data, _ := json.Marshal(map[string]any{
			"type":          event.Type,
			"text":          event.Text,
			"input_tokens":  event.InputTokens,
			"output_tokens": event.OutputTokens,
		})
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Type, data)
		flusher.Flush()
	}
}

// buildAISystemPrompt assembles the system prompt from conventions + page context.
func (s *Server) buildAISystemPrompt(mode, pageContent, pagePath string) string {
	prompt := "You are an AI assistant integrated into Gowiki, a wiki system.\n\n"

	// Add conventions (same content as GET /api/ai/v1/conventions, but as text).
	prompt += "# Wiki Conventions\n\n"
	prompt += "You must follow the Gowiki Markdown dialect strictly:\n"
	prompt += "- *italic* only (not _italic_)\n"
	prompt += "- _underline_ (not italic)\n"
	prompt += "- **bold** only\n"
	prompt += "- ATX headings (#) only\n"
	prompt += "- - for unordered lists (not *)\n"
	prompt += "- Raw HTML is forbidden\n"
	prompt += "- Single newline = hard line break in paragraphs\n\n"

	if mode == "action" {
		prompt += "# Mode: Action\n\n"
		prompt += "The user will give you an instruction. Apply the requested change directly.\n"
		prompt += "Output ONLY the modified page markdown. Do not include explanations or commentary outside the markdown.\n"
		prompt += "If you only need to change part of the page, output the full page with the change applied.\n\n"
	} else {
		prompt += "# Mode: Review\n\n"
		prompt += "The user wants a structured review of the page. Analyze the content and produce a numbered list of proposals.\n"
		prompt += "Each proposal must have:\n"
		prompt += "- Number\n"
		prompt += "- Location (section or line description)\n"
		prompt += "- Original text (exact quote)\n"
		prompt += "- Proposed text\n"
		prompt += "- Rationale (brief)\n\n"
		prompt += "Format each proposal as a JSON object in a JSON array. Example:\n"
		prompt += "```json\n"
		prompt += "[{\"number\": 1, \"location\": \"Section 3, paragraph 1\", \"original\": \"...\", \"proposed\": \"...\", \"rationale\": \"...\"}]\n"
		prompt += "```\n\n"
	}

	if pageContent != "" {
		prompt += "# Current Page Content\n\n"
		prompt += fmt.Sprintf("Page path: %s\n\n", pagePath)
		prompt += "```markdown\n"
		prompt += pageContent
		prompt += "\n```\n"
	}

	return prompt
}
