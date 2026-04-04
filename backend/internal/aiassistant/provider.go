// Package aiassistant implements the integrated AI assistant that proxies
// LLM requests for the browser-based chat panel.
package aiassistant

import "context"

// ChatRequest contains everything needed to make an LLM call.
type ChatRequest struct {
	SystemPrompt string    // conventions + mode instructions
	Messages     []Message // conversation history + current user message
	MaxTokens    int       // max response tokens
	Model        string    // model identifier (e.g. "claude-sonnet-4-20250514")
	Tools        []Tool    // available tools (optional)
}

// Message is a single conversation turn.
type Message struct {
	Role       string        `json:"role"`    // "user" or "assistant"
	Content    any           `json:"content"` // string or []ContentBlock
}

// ContentBlock is a structured content block (text, tool_use, tool_result).
type ContentBlock struct {
	Type      string `json:"type"`                 // "text", "tool_use", "tool_result"
	Text      string `json:"text,omitempty"`       // for "text" blocks
	ID        string `json:"id,omitempty"`         // for "tool_use" blocks
	Name      string `json:"name,omitempty"`       // for "tool_use" blocks
	Input     any    `json:"input,omitempty"`      // for "tool_use" blocks
	ToolUseID string `json:"tool_use_id,omitempty"` // for "tool_result" blocks
	Content   string `json:"content,omitempty"`    // for "tool_result" blocks (overloaded with Text)
}

// Tool describes a tool the AI can call.
type Tool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	InputSchema any    `json:"input_schema"`
}

// ChatEvent is a streaming event from the LLM.
type ChatEvent struct {
	Type string // "token", "done", "error", "tool_use"
	Text string // for "token": the text fragment; for "error": the error message
	// Token usage, populated only for "done" events.
	InputTokens  int
	OutputTokens int
	// Tool use info, populated only for "tool_use" events.
	ToolUseID string
	ToolName  string
	ToolInput map[string]any
}

// Provider abstracts the LLM backend. Implementations must support streaming.
type Provider interface {
	// Chat sends a request and returns a channel of streaming events.
	// The channel is closed when the response is complete.
	// The caller must drain the channel.
	Chat(ctx context.Context, req ChatRequest) (<-chan ChatEvent, error)
}
