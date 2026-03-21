package collab

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// Relay forwards Yjs sync/awareness messages between clients editing the same page.
// It is a dumb pipe — it does not parse or understand the Yjs document.
type Relay struct {
	mu    sync.RWMutex
	rooms map[string]*room // page path -> room
}

type room struct {
	clients map[*RelayClient]bool
}

// RelayClient represents one WebSocket connection in a collaborative editing session.
type RelayClient struct {
	Username    string
	DisplayName string
	Page        string
	conn        *websocket.Conn
	send        chan []byte
	ctx         context.Context
	cancel      context.CancelFunc
}

// NewRelay creates a new Yjs relay.
func NewRelay() *Relay {
	return &Relay{
		rooms: make(map[string]*room),
	}
}

// Join adds a client to a room and starts its read/write pumps.
// Blocks until the connection closes.
func (rl *Relay) Join(conn *websocket.Conn, page, username, displayName string) {
	ctx, cancel := context.WithCancel(context.Background())
	client := &RelayClient{
		Username:    username,
		DisplayName: displayName,
		Page:        page,
		conn:        conn,
		send:        make(chan []byte, 64),
		ctx:         ctx,
		cancel:      cancel,
	}

	rl.mu.Lock()
	r := rl.rooms[page]
	if r == nil {
		r = &room{clients: make(map[*RelayClient]bool)}
		rl.rooms[page] = r
	}
	r.clients[client] = true
	count := len(r.clients)
	rl.mu.Unlock()

	log.Printf("collab: %s joined %s (%d clients)", username, page, count)

	defer func() {
		rl.mu.Lock()
		r := rl.rooms[page]
		if r != nil {
			delete(r.clients, client)
			if len(r.clients) == 0 {
				delete(rl.rooms, page)
			}
		}
		rl.mu.Unlock()
		cancel()
		conn.Close(websocket.StatusNormalClosure, "")
		log.Printf("collab: %s left %s", username, page)
	}()

	go client.writePump()
	client.readPump(rl)
}

// broadcast sends a message to all clients in the same room except the sender.
func (rl *Relay) broadcast(page string, sender *RelayClient, data []byte) {
	rl.mu.RLock()
	r := rl.rooms[page]
	if r == nil {
		rl.mu.RUnlock()
		return
	}
	// Copy client list under read lock.
	targets := make([]*RelayClient, 0, len(r.clients))
	for c := range r.clients {
		if c != sender {
			targets = append(targets, c)
		}
	}
	rl.mu.RUnlock()

	for _, c := range targets {
		select {
		case c.send <- data:
		default:
			// Drop if buffer full — Yjs will resync.
		}
	}
}

func (c *RelayClient) readPump(rl *Relay) {
	c.conn.SetReadLimit(1 << 20) // 1 MB max message

	for {
		_, data, err := c.conn.Read(c.ctx)
		if err != nil {
			return
		}
		// Relay to all other clients in the same room.
		rl.broadcast(c.Page, c, data)
	}
}

func (c *RelayClient) writePump() {
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case msg, ok := <-c.send:
			if !ok {
				return
			}
			ctx, cancel := context.WithTimeout(c.ctx, 5*time.Second)
			err := c.conn.Write(ctx, websocket.MessageBinary, msg)
			cancel()
			if err != nil {
				return
			}

		case <-ticker.C:
			ctx, cancel := context.WithTimeout(c.ctx, 5*time.Second)
			err := c.conn.Ping(ctx)
			cancel()
			if err != nil {
				return
			}

		case <-c.ctx.Done():
			return
		}
	}
}
