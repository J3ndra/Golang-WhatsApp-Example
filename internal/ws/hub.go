// Package ws provides a WebSocket hub for real-time communication
// with the CS (Customer Service) dashboard frontend.
//
// The Hub follows the standard gorilla/websocket chat example pattern:
//   - Each connected CS browser tab is a *Client
//   - Messages are JSON-encoded WSEvent payloads
//   - Broadcast() is safe to call from any goroutine
package ws

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// upgrader converts an HTTP connection to a WebSocket connection.
// CheckOrigin is permissive here — tighten for production by checking
// r.Header.Get("Origin") against an allowlist.
var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // allow all origins; restrict in production
	},
}

// WSEvent is the JSON payload sent to CS panel clients over WebSocket.
type WSEvent struct {
	// EventType distinguishes the kind of event (e.g. "new_message", "escalated").
	EventType string `json:"event_type"`
	// Phone is the customer's WhatsApp phone number.
	Phone string `json:"phone"`
	// Name is the customer's display name (from Contacts table or "Customer").
	Name string `json:"name"`
	// SessionID is the active chat session UUID.
	SessionID string `json:"session_id"`
	// Message is the message body text.
	Message string `json:"message"`
	// Timestamp is when the event occurred (UTC, RFC3339).
	Timestamp string `json:"timestamp"`
}

// client represents a single WebSocket connection from the CS dashboard.
type client struct {
	hub  *Hub
	conn *websocket.Conn
	send chan []byte // buffered channel of outgoing messages
}

// Hub maintains the set of active WebSocket clients and broadcasts messages.
type Hub struct {
	mu      sync.RWMutex
	clients map[*client]struct{}
}

// NewHub creates and returns an initialised Hub.
func NewHub() *Hub {
	return &Hub{
		clients: make(map[*client]struct{}),
	}
}

// Broadcast marshals event to JSON and sends it to every connected client.
// Clients whose send buffer is full are dropped to avoid blocking.
func (h *Hub) Broadcast(event WSEvent) {
	data, err := json.Marshal(event)
	if err != nil {
		slog.Error("ws: failed to marshal event", "error", err)
		return
	}

	h.mu.RLock()
	defer h.mu.RUnlock()

	for c := range h.clients {
		select {
		case c.send <- data:
		default:
			// slow / disconnected client — drop and close
			slog.Warn("ws: client send buffer full, dropping client")
			close(c.send)
			delete(h.clients, c)
		}
	}
}

// ServeWS upgrades the HTTP request to a WebSocket and registers the client.
// The frontend CS panel connects to GET /ws.
func (h *Hub) ServeWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Error("ws: upgrade failed", "error", err)
		return
	}

	c := &client{
		hub:  h,
		conn: conn,
		send: make(chan []byte, 256),
	}

	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()

	slog.Info("ws: client connected", "remote_addr", conn.RemoteAddr().String())

	// writePump — sends queued messages to the WebSocket connection.
	go func() {
		defer func() {
			conn.Close()
			h.mu.Lock()
			delete(h.clients, c)
			h.mu.Unlock()
			slog.Info("ws: client disconnected", "remote_addr", conn.RemoteAddr().String())
		}()

		pingTicker := time.NewTicker(30 * time.Second)
		defer pingTicker.Stop()

		for {
			select {
			case msg, ok := <-c.send:
				// Set a write deadline so a stuck client cannot block the goroutine.
				conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
				if !ok {
					// Hub closed the channel.
					conn.WriteMessage(websocket.CloseMessage, []byte{})
					return
				}
				if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
					slog.Warn("ws: write error", "error", err)
					return
				}

			case <-pingTicker.C:
				conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
				if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
					return
				}
			}
		}
	}()

	// readPump — discards inbound frames but keeps the connection alive
	// and handles pong/close control frames.
	go func() {
		defer conn.Close()
		conn.SetReadLimit(512)
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		conn.SetPongHandler(func(string) error {
			conn.SetReadDeadline(time.Now().Add(60 * time.Second))
			return nil
		})
		for {
			// Read and discard; we only push to the client, not the other way.
			if _, _, err := conn.ReadMessage(); err != nil {
				break
			}
		}
	}()
}
