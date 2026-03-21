// Package collab provides real-time presence tracking for wiki pages.
// Users connect via WebSocket and the hub broadcasts who is viewing or
// editing each page.
package collab

import (
	"encoding/json"
	"sync"
	"time"
)

// UserPresence represents a user's current state on a page.
type UserPresence struct {
	Username    string `json:"username"`
	DisplayName string `json:"display_name,omitempty"`
	Page        string `json:"page"`
	Mode        string `json:"mode"`   // "view" or "edit"
	Offset      int    `json:"offset"` // cursor offset in markdown string (-1 if unknown)
	Since       int64  `json:"since"`  // unix ms
}

// PresenceUpdate is sent to clients when presence changes on a page.
type PresenceUpdate struct {
	Type  string         `json:"type"`  // "presence"
	Page  string         `json:"page"`
	Users []UserPresence `json:"users"`
}

// Hub manages all active WebSocket connections and presence state.
type Hub struct {
	mu      sync.RWMutex
	clients map[*Client]bool
	// page -> username -> presence
	pages map[string]map[string]*clientPresence
}

type clientPresence struct {
	client   *Client
	presence UserPresence
}

// NewHub creates a new presence hub.
func NewHub() *Hub {
	return &Hub{
		clients: make(map[*Client]bool),
		pages:   make(map[string]map[string]*clientPresence),
	}
}

// Register adds a client to the hub.
func (h *Hub) Register(c *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.clients[c] = true
}

// Unregister removes a client and clears its presence from all pages.
func (h *Hub) Unregister(c *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()

	delete(h.clients, c)

	// Remove from all pages and notify.
	var affectedPages []string
	for page, users := range h.pages {
		if _, ok := users[c.Username]; ok {
			if users[c.Username].client == c {
				delete(users, c.Username)
				if len(users) == 0 {
					delete(h.pages, page)
				}
				affectedPages = append(affectedPages, page)
			}
		}
	}

	// Broadcast updates for affected pages (outside the lock would be
	// better for performance, but presence updates are small and infrequent).
	for _, page := range affectedPages {
		h.broadcastPageLocked(page)
	}
}

// SetPresence updates a client's presence on a page.
// If the client was previously on a different page, it is removed from that page.
func (h *Hub) SetPresence(c *Client, page, mode string, offset int) {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Remove from previous page if different.
	var prevPage string
	for p, users := range h.pages {
		if cp, ok := users[c.Username]; ok && cp.client == c && p != page {
			prevPage = p
			delete(users, c.Username)
			if len(users) == 0 {
				delete(h.pages, p)
			}
			break
		}
	}

	// Add to new page.
	if h.pages[page] == nil {
		h.pages[page] = make(map[string]*clientPresence)
	}
	h.pages[page][c.Username] = &clientPresence{
		client: c,
		presence: UserPresence{
			Username:    c.Username,
			DisplayName: c.DisplayName,
			Page:        page,
			Mode:        mode,
			Offset:      offset,
			Since:       time.Now().UnixMilli(),
		},
	}

	// Broadcast to affected pages.
	if prevPage != "" {
		h.broadcastPageLocked(prevPage)
	}
	h.broadcastPageLocked(page)
}

// GetPagePresence returns current presence for a page.
func (h *Hub) GetPagePresence(page string) []UserPresence {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.pagePresenceLocked(page)
}

func (h *Hub) pagePresenceLocked(page string) []UserPresence {
	users := h.pages[page]
	if len(users) == 0 {
		return nil
	}
	result := make([]UserPresence, 0, len(users))
	for _, cp := range users {
		result = append(result, cp.presence)
	}
	return result
}

func (h *Hub) broadcastPageLocked(page string) {
	update := PresenceUpdate{
		Type:  "presence",
		Page:  page,
		Users: h.pagePresenceLocked(page),
	}
	if update.Users == nil {
		update.Users = []UserPresence{}
	}
	data, err := json.Marshal(update)
	if err != nil {
		return
	}

	// Send to all clients on this page.
	for _, cp := range h.pages[page] {
		cp.client.Send(data)
	}

	// Also send to clients that were on this page (they may have left — send
	// the empty update so their UI clears). We do this by sending to all
	// clients and letting the client filter by page.
	for c := range h.clients {
		c.Send(data)
	}
}
