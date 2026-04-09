package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"gowiki/backend/internal/aiassistant"
)

// reinitAIProvider re-creates the AI provider after a config change.
func (s *Server) reinitAIProvider() {
	cfg := s.configStore.Get()
	if !cfg.AIAssistant.Enabled {
		s.aiProvider = nil
		s.aiRateLimiter = nil
		return
	}
	apiKey := cfg.AIAssistant.EffectiveAPIKey()
	if apiKey == "" {
		s.aiProvider = nil
		s.aiRateLimiter = nil
		return
	}
	switch cfg.AIAssistant.Provider {
	case "anthropic", "":
		s.aiProvider = aiassistant.NewAnthropicProvider(apiKey)
		if s.aiRateLimiter == nil {
			s.aiRateLimiter = aiassistant.NewUserRateLimiter()
		}
	default:
		s.aiProvider = nil
		s.aiRateLimiter = nil
	}
}

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
//
// In action mode: streams tokens for progress, then sends a final "edits" event
// with structured diffs between the original and modified page.
//
// In review mode: streams the full AI response as tokens, which contains
// a JSON array of numbered proposals.
func (s *Server) handleAIChat(w http.ResponseWriter, r *http.Request) {
	if s.aiProvider == nil {
		writeError(w, http.StatusServiceUnavailable, "AI provider not configured")
		return
	}

	var req struct {
		PagePath string `json:"page_path"`
		Message  string `json:"message"`
		Mode     string `json:"mode"` // "action", "review", or "question"
		History  []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"history"`
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
	if req.Mode != "action" && req.Mode != "review" && req.Mode != "question" {
		writeError(w, http.StatusBadRequest, "mode must be 'action', 'review', or 'question'")
		return
	}

	username := UsernameFromContext(r.Context())
	cfg := s.configStore.Get()

	// Rate limiting.
	if s.aiRateLimiter != nil {
		ok, reason := s.aiRateLimiter.Allow(
			username,
			cfg.AIAssistant.Costs.RateLimitPerUser,
			cfg.AIAssistant.Costs.DailyLimitPerUser,
		)
		if !ok {
			writeError(w, http.StatusTooManyRequests, reason)
			return
		}
	}

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

	// For question mode, build page listing and provide read_page tool.
	var pageListing string
	var tools []aiassistant.Tool
	if req.Mode == "question" {
		// Only include listing on first message (no history = first turn).
		if len(req.History) == 0 {
			if lister, ok := s.store.(SitemapLister); ok {
				if allPages, err := lister.ListAllPages(); err == nil {
					var sb strings.Builder
					for _, p := range allPages {
						if s.aclStore != nil && !s.aclStore.CheckPermission("@ai", nil, p.Path, "view") {
							continue
						}
						sb.WriteString(fmt.Sprintf("- [%s](%s)\n", p.Title, p.Path))
					}
					pageListing = sb.String()
				}
			}
		}
		tools = []aiassistant.Tool{{
			Name:        "read_page",
			Description: "Read the full content of a wiki page. Use this to answer questions that require specific page content.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{
						"type":        "string",
						"description": "The wiki page path, e.g. /regulatory/qms/soft/sop01/",
					},
				},
				"required": []string{"path"},
			},
		}}
	}

	// Build system prompt.
	systemPrompt := buildAISystemPrompt(req.Mode, pageContent, req.PagePath, pageListing)

	// Build LLM messages: history + current message.
	var messages []aiassistant.Message
	for _, h := range req.History {
		if h.Role == "user" || h.Role == "assistant" {
			messages = append(messages, aiassistant.Message{Role: h.Role, Content: h.Content})
		}
	}
	// For the first question, prepend the page listing as a user context message.
	if pageListing != "" && len(req.History) == 0 {
		messages = append(messages,
			aiassistant.Message{Role: "user", Content: "Here is the listing of all wiki pages for reference:\n\n" + pageListing},
			aiassistant.Message{Role: "assistant", Content: "I have the wiki page listing. I can also use `read_page` to access any page's full content. How can I help?"},
		)
	}
	messages = append(messages, aiassistant.Message{Role: "user", Content: req.Message})

	// Build LLM request.
	chatReq := aiassistant.ChatRequest{
		SystemPrompt: systemPrompt,
		Messages:     messages,
		MaxTokens:    cfg.AIAssistant.MaxTokens,
		Model:        cfg.AIAssistant.Model,
		Tools:        tools,
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

	// Tool use loop: call provider, handle tool_use responses, repeat.
	var fullResponse strings.Builder
	var inputTokens, outputTokens int
	maxToolRounds := 10 // safety limit only

	for round := 0; round <= maxToolRounds; round++ {
		eventCh, err := s.aiProvider.Chat(r.Context(), chatReq)
		if err != nil {
			writeError(w, http.StatusBadGateway, fmt.Sprintf("AI provider error: %v", err))
			return
		}

		var pendingToolUses []aiassistant.ChatEvent
		roundDone := false

		for event := range eventCh {
			switch event.Type {
			case "token":
				fullResponse.WriteString(event.Text)
				data, _ := json.Marshal(map[string]any{
					"type": "token",
					"text": event.Text,
				})
				fmt.Fprintf(w, "event: token\ndata: %s\n\n", data)
				flusher.Flush()

			case "tool_use":
				pendingToolUses = append(pendingToolUses, event)
				// Notify frontend that the AI is reading a page.
				data, _ := json.Marshal(map[string]any{
					"type": "token",
					"text": fmt.Sprintf("\n*Reading %s...*\n", event.ToolInput["path"]),
				})
				fmt.Fprintf(w, "event: token\ndata: %s\n\n", data)
				flusher.Flush()

			case "done":
				inputTokens += event.InputTokens
				outputTokens += event.OutputTokens
				roundDone = true

			case "error":
				data, _ := json.Marshal(map[string]any{
					"type": "error",
					"text": event.Text,
				})
				fmt.Fprintf(w, "event: error\ndata: %s\n\n", data)
				flusher.Flush()
				return
			}
		}

		// If no tool uses, we're done.
		if len(pendingToolUses) == 0 || !roundDone {
			break
		}

		// Execute tools and build follow-up messages.
		// First, add the assistant's response (with tool_use blocks) to messages.
		var assistantBlocks []aiassistant.ContentBlock
		if text := fullResponse.String(); text != "" {
			assistantBlocks = append(assistantBlocks, aiassistant.ContentBlock{
				Type: "text",
				Text: text,
			})
		}
		for _, tu := range pendingToolUses {
			assistantBlocks = append(assistantBlocks, aiassistant.ContentBlock{
				Type:  "tool_use",
				ID:    tu.ToolUseID,
				Name:  tu.ToolName,
				Input: tu.ToolInput,
			})
		}
		chatReq.Messages = append(chatReq.Messages, aiassistant.Message{
			Role:    "assistant",
			Content: assistantBlocks,
		})

		// Execute each tool and add results.
		var toolResults []aiassistant.ContentBlock
		for _, tu := range pendingToolUses {
			result := s.executeAITool(tu.ToolName, tu.ToolInput, username)
			toolResults = append(toolResults, aiassistant.ContentBlock{
				Type:      "tool_result",
				ToolUseID: tu.ToolUseID,
				Content:   result,
			})
		}
		chatReq.Messages = append(chatReq.Messages, aiassistant.Message{
			Role:    "user",
			Content: toolResults,
		})

		// Reset response buffer for the next round.
		fullResponse.Reset()
	}

	// For action/review modes: try to parse proposals from the AI response and
	// insert flow markers into the page content for verified proposals.
	if pageContent != "" && req.Mode != "question" {
		proposals := parseAIProposals(fullResponse.String())
		if len(proposals) > 0 {
			markedContent, verified := insertProposalMarkers(pageContent, proposals)
			if len(verified) > 0 {
				data, _ := json.Marshal(map[string]any{
					"type":      "markers",
					"markdown":  markedContent,
					"proposals": verified,
				})
				fmt.Fprintf(w, "event: markers\ndata: %s\n\n", data)
				flusher.Flush()
			}
		}
	}

	// Send done event with usage.
	data, _ := json.Marshal(map[string]any{
		"type":          "done",
		"input_tokens":  inputTokens,
		"output_tokens": outputTokens,
	})
	fmt.Fprintf(w, "event: done\ndata: %s\n\n", data)
	flusher.Flush()
}

// executeAITool runs a tool call and returns the result as text.
func (s *Server) executeAITool(name string, input map[string]any, username string) string {
	switch name {
	case "read_page":
		pagePath, _ := input["path"].(string)
		if pagePath == "" {
			return "Error: path is required"
		}
		pagePath = strings.TrimSpace(pagePath)
		// Check @ai ACL.
		if s.aclStore != nil && !s.aclStore.CheckPermission("@ai", nil, pagePath, "view") {
			return "Error: AI access denied for this page"
		}
		// Check user ACL.
		groups := s.effectiveGroups(username)
		if s.aclStore != nil && !s.aclStore.CheckPermission(username, groups, pagePath, "view") {
			return "Error: access denied"
		}
		page, err := s.store.Get(pagePath)
		if err != nil {
			return "Error: page not found"
		}
		return page.Markdown
	default:
		return fmt.Sprintf("Error: unknown tool %q", name)
	}
}

// aiProposal represents a single AI-generated change proposal.
type aiProposal struct {
	Number    int    `json:"number"`
	Location  string `json:"location"`
	Original  string `json:"original"`
	Proposed  string `json:"proposed"`
	Rationale string `json:"rationale"`
	Marker    string `json:"marker,omitempty"`    // marker ID (set by backend)
	Verified  bool   `json:"verified,omitempty"`  // true if original was found in content
}

// parseAIProposals extracts a JSON array of proposals from the AI response.
func parseAIProposals(response string) []aiProposal {
	// Find JSON array in the response.
	start := strings.Index(response, "[")
	if start == -1 {
		return nil
	}
	end := strings.LastIndex(response, "]")
	if end == -1 || end <= start {
		return nil
	}
	var proposals []aiProposal
	if err := json.Unmarshal([]byte(response[start:end+1]), &proposals); err != nil {
		return nil
	}
	return proposals
}

// insertProposalMarkers locates each proposal's original text in the content
// and wraps it with flow markers. Returns the modified content and the list
// of verified proposals (with marker IDs assigned).
func insertProposalMarkers(content string, proposals []aiProposal) (string, []aiProposal) {
	type positioned struct {
		idx      int
		proposal *aiProposal
	}

	var found []positioned
	for i := range proposals {
		p := &proposals[i]
		if p.Original == "" {
			continue
		}
		idx := strings.Index(content, p.Original)
		if idx < 0 {
			continue
		}
		// Check for ambiguity.
		if strings.Index(content[idx+1:], p.Original) >= 0 {
			continue // ambiguous, skip
		}
		markerID := fmt.Sprintf("p%d", p.Number)
		p.Marker = markerID
		p.Verified = true
		found = append(found, positioned{idx: idx, proposal: p})
	}

	if len(found) == 0 {
		return content, nil
	}

	// Sort by position descending so we can insert markers without shifting.
	for i := 0; i < len(found)-1; i++ {
		for j := i + 1; j < len(found); j++ {
			if found[j].idx > found[i].idx {
				found[i], found[j] = found[j], found[i]
			}
		}
	}

	// Check for overlaps — skip overlapping proposals.
	result := content
	var verified []aiProposal
	lastStart := len(result)
	for _, f := range found {
		endPos := f.idx + len(f.proposal.Original)
		if endPos > lastStart {
			continue // overlaps with a previously inserted marker
		}
		closeMarker := fmt.Sprintf("{#/%s}", f.proposal.Marker)
		openMarker := fmt.Sprintf("{#%s}", f.proposal.Marker)
		result = result[:endPos] + closeMarker + result[endPos:]
		result = result[:f.idx] + openMarker + result[f.idx:]
		lastStart = f.idx
		verified = append(verified, *f.proposal)
	}

	// Reverse verified to get ascending order.
	for i, j := 0, len(verified)-1; i < j; i, j = i+1, j-1 {
		verified[i], verified[j] = verified[j], verified[i]
	}

	return result, verified
}

// buildAISystemPrompt assembles the system prompt from conventions + page context.
func buildAISystemPrompt(mode, pageContent, pagePath, _ string) string {
	var b strings.Builder

	b.WriteString("You are an AI assistant integrated into Gowiki, a wiki system. ")
	b.WriteString("You operate on the page the user is currently editing.\n\n")

	// Full conventions.
	b.WriteString("# Gowiki Markdown Dialect\n\n")
	b.WriteString("You MUST follow these rules strictly. The dialect is bijective — one canonical syntax per node type.\n\n")
	b.WriteString("## Syntax rules\n")
	b.WriteString("- `*italic*` only — `_text_` means UNDERLINE, not italic\n")
	b.WriteString("- `**bold**` only — `__bold__` is rejected\n")
	b.WriteString("- `_underline_` — produces underline, NOT italic\n")
	b.WriteString("- `~~strikethrough~~`\n")
	b.WriteString("- `~subscript~`, `^superscript^`\n")
	b.WriteString("- `^[inline footnote]` — supports bold, italic, links inside\n")
	b.WriteString("- ATX headings only (`# H1`, `## H2`) — setext headings rejected\n")
	b.WriteString("- `- item` for unordered lists — `*` as list marker is rejected\n")
	b.WriteString("- `1. item` for ordered lists\n")
	b.WriteString("- Numbered headings: `## 1. Title` (prefix syntax, not directive)\n")
	b.WriteString("- Raw HTML is forbidden — `<` and `>` are plain characters\n")
	b.WriteString("- HTML entities are not interpreted — use UTF-8 directly\n")
	b.WriteString("- Single newline in a paragraph = hard line break\n")
	b.WriteString("- Trailing spaces have no meaning\n")
	b.WriteString("- `\\n` literal = line break in lists and tables only\n")
	b.WriteString("- Pipe tables — no column alignment syntax\n")
	b.WriteString("- Directives: `{name key=value}` on its own line before the target block\n\n")

	b.WriteString("## Forbidden\n")
	b.WriteString("- Do NOT use `_text_` for italic\n")
	b.WriteString("- Do NOT use `*` as a list marker\n")
	b.WriteString("- Do NOT use raw HTML\n")
	b.WriteString("- Do NOT use HTML entities\n")
	b.WriteString("- Do NOT use setext headings\n")
	b.WriteString("- Do NOT remove or reformat content you were not asked to change\n")
	b.WriteString("- Do NOT silently change document structure\n")
	b.WriteString("- Do NOT renumber headings — numbered headings (`## 1. Title`) are managed by the wiki system and must not be modified\n")
	b.WriteString("- Do NOT suggest changes to heading numbers, section numbers, or list numbering\n\n")

	// Mode-specific instructions.
	switch mode {
	case "action":
		b.WriteString("# Instructions\n\n")
		b.WriteString("The user will give you an instruction to modify the page.\n")
		b.WriteString("Return ONLY the targeted edits as a JSON array — do NOT return the full page.\n")
		b.WriteString("Each edit replaces an exact region of the page. Only include the parts that change.\n")
		b.WriteString("The `original` field must be an EXACT verbatim copy-paste from the page content.\n")
		b.WriteString("For insertions, use a small surrounding context as `original` (e.g. the line before and after the insertion point) and include it in `proposed` with the new content added.\n\n")
		b.WriteString("Output ONLY a JSON array. No prose before or after.\n")
		b.WriteString("Each edit is a JSON object with these fields:\n")
		b.WriteString("- `original` (string): EXACT verbatim text from the page to be replaced\n")
		b.WriteString("- `proposed` (string): replacement text\n")
		b.WriteString("- `rationale` (string): brief explanation of the change\n\n")
		b.WriteString("Example — adding a row to a table:\n")
		b.WriteString("```json\n")
		b.WriteString(`[{"original": "| Alice | Admin |\n| Bob | Editor |", "proposed": "| Alice | Admin |\n| Bob | Editor |\n| Carol | Viewer |", "rationale": "Added Carol as viewer"}]`)
		b.WriteString("\n```\n\n")

	case "review":
		b.WriteString("# Instructions\n\n")
		b.WriteString("The user will tell you what to review. Propose changes accordingly.\n")
		b.WriteString("The `original` field must be an EXACT verbatim copy-paste from the page content — the exact characters as they appear. If you cannot match exactly, do not propose the change.\n")
		b.WriteString("If the page is correct for what was asked, return an empty array [].\n\n")
		b.WriteString("Output ONLY a JSON array. No prose before or after.\n")
		b.WriteString("Each proposal is a JSON object with these fields:\n")
		b.WriteString("- `number` (int): sequential proposal number\n")
		b.WriteString("- `location` (string): section or paragraph description\n")
		b.WriteString("- `original` (string): EXACT verbatim text from the page to be replaced\n")
		b.WriteString("- `proposed` (string): replacement text\n")
		b.WriteString("- `rationale` (string): brief explanation\n\n")
		b.WriteString("Example:\n")
		b.WriteString("```json\n")
		b.WriteString(`[{"number": 1, "location": "Section 3, paragraph 1", "original": "The datas shows", "proposed": "The data show", "rationale": "Grammar: 'data' is plural"}]`)
		b.WriteString("\n```\n\n")

	case "question":
		b.WriteString("# Instructions\n\n")
		b.WriteString("The user is asking a question about the wiki or its content.\n")
		b.WriteString("Answer concisely and helpfully. Use markdown formatting in your response.\n")
		b.WriteString("When referencing wiki pages, use markdown links with the page path: [Page Title](/path/to/page).\n")
		b.WriteString("You have access to the wiki page listing (provided at the start of the conversation).\n")
		b.WriteString("You can use the `read_page` tool to read the full content of any page when needed.\n")
		b.WriteString("Do not modify any page content — just answer the question.\n\n")
	}

	// Page content as context, wrapped in XML tags so code fences inside
	// the content are not confused with prompt formatting.
	if pageContent != "" {
		b.WriteString("# Current Page\n\n")
		b.WriteString(fmt.Sprintf("Path: `%s`\n\n", pagePath))
		b.WriteString("The page content is enclosed in <page-content> tags. Everything between these tags is the raw markdown of the page, verbatim.\n\n")
		b.WriteString("<page-content>\n")
		b.WriteString(pageContent)
		b.WriteString("\n</page-content>\n")
	}

	return b.String()
}
