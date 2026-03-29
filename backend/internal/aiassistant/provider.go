// Package aiassistant implements the integrated AI assistant that proxies
// LLM requests for the browser-based chat panel.
package aiassistant

import "context"

// ChatRequest contains everything needed to make an LLM call.
type ChatRequest struct {
	SystemPrompt string          // conventions + mode instructions
	Messages     []Message       // conversation history + current user message
	MaxTokens    int             // max response tokens
	Model        string          // model identifier (e.g. "claude-sonnet-4-20250514")
}

// Message is a single conversation turn.
type Message struct {
	Role    string `json:"role"`    // "user" or "assistant"
	Content string `json:"content"`
}

// ChatEvent is a streaming event from the LLM.
type ChatEvent struct {
	Type string // "token", "done", "error"
	Text string // for "token": the text fragment; for "error": the error message
	// Token usage, populated only for "done" events.
	InputTokens  int
	OutputTokens int
}

// Provider abstracts the LLM backend. Implementations must support streaming.
type Provider interface {
	// Chat sends a request and returns a channel of streaming events.
	// The channel is closed when the response is complete.
	// The caller must drain the channel.
	Chat(ctx context.Context, req ChatRequest) (<-chan ChatEvent, error)
}
