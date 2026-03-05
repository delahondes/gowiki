package todo

import (
	"fmt"
	"net/http"
	"sync"
	"encoding/json"
)

// Hub is an in-process pub-sub for SSE task events.
type Hub struct {
	mu          sync.RWMutex
	subscribers map[string]map[chan Event]struct{} // userID → set of channels
}

// NewHub creates a new event hub.
func NewHub() *Hub {
	return &Hub{
		subscribers: make(map[string]map[chan Event]struct{}),
	}
}

// Subscribe creates a channel for receiving events for a user.
// Returns the channel and a cancel function.
func (h *Hub) Subscribe(userID string) (<-chan Event, func()) {
	ch := make(chan Event, 16)
	h.mu.Lock()
	if h.subscribers[userID] == nil {
		h.subscribers[userID] = make(map[chan Event]struct{})
	}
	h.subscribers[userID][ch] = struct{}{}
	h.mu.Unlock()

	cancel := func() {
		h.mu.Lock()
		delete(h.subscribers[userID], ch)
		if len(h.subscribers[userID]) == 0 {
			delete(h.subscribers, userID)
		}
		h.mu.Unlock()
		close(ch)
	}
	return ch, cancel
}

// Publish sends an event to all subscribers for a user. Non-blocking.
func (h *Hub) Publish(userID string, event Event) {
	h.mu.RLock()
	subs := h.subscribers[userID]
	h.mu.RUnlock()

	for ch := range subs {
		select {
		case ch <- event:
		default:
			// Drop if slow consumer.
		}
	}
}

// PublishAll sends an event to all connected users. Non-blocking.
func (h *Hub) PublishAll(event Event) {
	h.mu.RLock()
	allSubs := make(map[chan Event]struct{})
	for _, subs := range h.subscribers {
		for ch := range subs {
			allSubs[ch] = struct{}{}
		}
	}
	h.mu.RUnlock()

	for ch := range allSubs {
		select {
		case ch <- event:
		default:
		}
	}
}

// HandleStream is an HTTP handler for SSE connections.
func (h *Hub) HandleStream(w http.ResponseWriter, r *http.Request, userID string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ch, cancel := h.Subscribe(userID)
	defer cancel()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-ch:
			if !ok {
				return
			}
			data, err := json.Marshal(event)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Type, data)
			flusher.Flush()
		}
	}
}
