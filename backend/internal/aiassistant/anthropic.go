package aiassistant

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const anthropicAPIURL = "https://api.anthropic.com/v1/messages"
const anthropicAPIVersion = "2023-06-01"

// AnthropicProvider implements Provider using the Anthropic Messages API.
type AnthropicProvider struct {
	apiKey string
	client *http.Client
}

// NewAnthropicProvider creates a provider for the Anthropic Claude API.
func NewAnthropicProvider(apiKey string) *AnthropicProvider {
	return &AnthropicProvider{
		apiKey: apiKey,
		client: &http.Client{},
	}
}

type anthropicRequest struct {
	Model     string              `json:"model"`
	MaxTokens int                 `json:"max_tokens"`
	System    []anthropicSysBlock `json:"system,omitempty"`
	Messages  []json.RawMessage   `json:"messages"`
	Stream    bool                `json:"stream"`
	Tools     []Tool              `json:"tools,omitempty"`
}

type anthropicSysBlock struct {
	Type         string                 `json:"type"`
	Text         string                 `json:"text"`
	CacheControl *anthropicCacheControl `json:"cache_control,omitempty"`
}

type anthropicCacheControl struct {
	Type string `json:"type"`
}

// Chat implements Provider.Chat with SSE streaming.
func (p *AnthropicProvider) Chat(ctx context.Context, req ChatRequest) (<-chan ChatEvent, error) {
	// Build messages as raw JSON to support both string and structured content.
	var msgs []json.RawMessage
	for _, m := range req.Messages {
		raw, err := json.Marshal(m)
		if err != nil {
			return nil, fmt.Errorf("marshal message: %w", err)
		}
		msgs = append(msgs, raw)
	}

	body := anthropicRequest{
		Model:     req.Model,
		MaxTokens: req.MaxTokens,
		System: []anthropicSysBlock{{
			Type:         "text",
			Text:         req.SystemPrompt,
			CacheControl: &anthropicCacheControl{Type: "ephemeral"},
		}},
		Messages: msgs,
		Stream:   true,
		Tools:    req.Tools,
	}

	bodyJSON, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", anthropicAPIURL, bytes.NewReader(bodyJSON))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", anthropicAPIVersion)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		errBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("anthropic API error (status %d): %s", resp.StatusCode, string(errBody))
	}

	ch := make(chan ChatEvent, 16)
	go p.readSSE(resp.Body, ch)
	return ch, nil
}

// readSSE parses the Anthropic SSE stream and sends events to the channel.
func (p *AnthropicProvider) readSSE(body io.ReadCloser, ch chan<- ChatEvent) {
	defer close(ch)
	defer body.Close()

	var inputTokens, outputTokens int
	var currentToolUse *ChatEvent // accumulates tool_use input JSON

	scanner := bufio.NewScanner(body)
	// Increase buffer for large responses.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()

		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")

		var event struct {
			Type    string `json:"type"`
			Index   int    `json:"index"`
			Delta   struct {
				Type        string `json:"type"`
				Text        string `json:"text"`
				PartialJSON string `json:"partial_json"`
			} `json:"delta"`
			ContentBlock struct {
				Type  string `json:"type"`
				ID    string `json:"id"`
				Name  string `json:"name"`
				Input json.RawMessage `json:"input"`
			} `json:"content_block"`
			Message struct {
				Usage struct {
					InputTokens  int `json:"input_tokens"`
					OutputTokens int `json:"output_tokens"`
				} `json:"usage"`
			} `json:"message"`
			Usage struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}

		switch event.Type {
		case "content_block_start":
			if event.ContentBlock.Type == "tool_use" {
				currentToolUse = &ChatEvent{
					Type:      "tool_use",
					ToolUseID: event.ContentBlock.ID,
					ToolName:  event.ContentBlock.Name,
				}
			}

		case "content_block_delta":
			if event.Delta.Type == "text_delta" && event.Delta.Text != "" {
				ch <- ChatEvent{Type: "token", Text: event.Delta.Text}
			}
			if event.Delta.Type == "input_json_delta" && currentToolUse != nil {
				// Accumulate partial JSON for tool input.
				currentToolUse.Text += event.Delta.PartialJSON
			}

		case "content_block_stop":
			if currentToolUse != nil {
				// Parse the accumulated tool input JSON.
				var input map[string]any
				if err := json.Unmarshal([]byte(currentToolUse.Text), &input); err == nil {
					currentToolUse.ToolInput = input
				}
				currentToolUse.Text = ""
				ch <- *currentToolUse
				currentToolUse = nil
			}

		case "message_start":
			if event.Message.Usage.InputTokens > 0 {
				inputTokens = event.Message.Usage.InputTokens
			}

		case "message_delta":
			if event.Usage.OutputTokens > 0 {
				outputTokens = event.Usage.OutputTokens
			}

		case "message_stop":
			ch <- ChatEvent{
				Type:         "done",
				InputTokens:  inputTokens,
				OutputTokens: outputTokens,
			}
			return

		case "error":
			ch <- ChatEvent{Type: "error", Text: data}
			return
		}
	}
}
