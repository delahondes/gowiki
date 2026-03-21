package collab

import (
	"context"
	"encoding/json"
	"time"

	"github.com/coder/websocket"
)

const (
	writeTimeout = 5 * time.Second
	pingInterval = 20 * time.Second
	maxMsgSize   = 4096
)

// Client represents a single WebSocket connection.
type Client struct {
	Username    string
	DisplayName string
	hub         *Hub
	conn        *websocket.Conn
	send        chan []byte
	ctx         context.Context
	cancel      context.CancelFunc
}

// NewClient creates a client from an accepted WebSocket connection.
func NewClient(hub *Hub, conn *websocket.Conn, username, displayName string) *Client {
	ctx, cancel := context.WithCancel(context.Background())
	return &Client{
		Username:    username,
		DisplayName: displayName,
		hub:         hub,
		conn:        conn,
		send:        make(chan []byte, 32),
		ctx:         ctx,
		cancel:      cancel,
	}
}

// Send queues a message for delivery. Non-blocking; drops if buffer is full.
func (c *Client) Send(data []byte) {
	select {
	case c.send <- data:
	default:
		// Buffer full — drop message (presence is best-effort).
	}
}

// Run starts the read and write loops. Blocks until the connection closes.
func (c *Client) Run() {
	c.hub.Register(c)
	defer func() {
		c.hub.Unregister(c)
		c.cancel()
		c.conn.Close(websocket.StatusNormalClosure, "")
	}()

	go c.writePump()
	c.readPump()
}

// clientMessage is the JSON structure clients send to the server.
type clientMessage struct {
	Type   string `json:"type"` // "join", "leave", "mode"
	Page   string `json:"page,omitempty"`
	Mode   string `json:"mode,omitempty"`   // "view" or "edit"
	Offset int    `json:"offset,omitempty"` // cursor offset in markdown (-1 = unknown)
}

func (c *Client) readPump() {
	c.conn.SetReadLimit(maxMsgSize)

	for {
		_, data, err := c.conn.Read(c.ctx)
		if err != nil {
			return
		}

		var msg clientMessage
		if json.Unmarshal(data, &msg) != nil {
			continue
		}

		switch msg.Type {
		case "join":
			if msg.Page == "" {
				continue
			}
			mode := msg.Mode
			if mode == "" {
				mode = "view"
			}
			c.hub.SetPresence(c, msg.Page, mode, msg.Offset)

		case "mode":
			if msg.Page == "" || msg.Mode == "" {
				continue
			}
			c.hub.SetPresence(c, msg.Page, msg.Mode, msg.Offset)

		case "leave":
			// Client navigated away — remove from current page.
			c.hub.mu.Lock()
			var prevPage string
			for p, users := range c.hub.pages {
				if cp, ok := users[c.Username]; ok && cp.client == c {
					prevPage = p
					delete(users, c.Username)
					if len(users) == 0 {
						delete(c.hub.pages, p)
					}
					break
				}
			}
			if prevPage != "" {
				c.hub.broadcastPageLocked(prevPage)
			}
			c.hub.mu.Unlock()

		case "ping":
			// Client-initiated keepalive — respond with pong.
			c.Send([]byte(`{"type":"pong"}`))
		}
	}
}

func (c *Client) writePump() {
	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()

	for {
		select {
		case msg, ok := <-c.send:
			if !ok {
				return
			}
			ctx, cancel := context.WithTimeout(c.ctx, writeTimeout)
			err := c.conn.Write(ctx, websocket.MessageText, msg)
			cancel()
			if err != nil {
				return
			}

		case <-ticker.C:
			// Server-initiated ping via the WebSocket ping frame.
			ctx, cancel := context.WithTimeout(c.ctx, writeTimeout)
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

