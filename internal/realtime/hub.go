package realtime

import (
	"encoding/json"
	"sync"

	"github.com/gorilla/websocket"
)

// Client represents a WebSocket connection
type Client struct {
	Conn   *websocket.Conn
	Send   chan []byte
	Filter map[string]string // query params as filter, e.g. {"city": "Jakarta"}
}

// Hub manages all WebSocket clients for a specific channel
type Hub struct {
	mu      sync.RWMutex
	clients map[*Client]bool
}

func NewHub() *Hub {
	return &Hub{
		clients: make(map[*Client]bool),
	}
}

func (h *Hub) Register(c *Client) {
	h.mu.Lock()
	h.clients[c] = true
	h.mu.Unlock()
}

func (h *Hub) Unregister(c *Client) {
	h.mu.Lock()
	if _, ok := h.clients[c]; ok {
		delete(h.clients, c)
		close(c.Send)
	}
	h.mu.Unlock()
}

// Broadcast sends a message to all connected clients
func (h *Hub) Broadcast(payload interface{}) {
	data, err := json.Marshal(WSMessage{Type: "update", Data: payload})
	if err != nil {
		return
	}

	h.mu.RLock()
	defer h.mu.RUnlock()

	for client := range h.clients {
		select {
		case client.Send <- data:
		default:
			// Client slow / disconnected — skip
		}
	}
}

// BroadcastFiltered sends only to clients whose filter matches
func (h *Hub) BroadcastFiltered(filterKey, filterValue string, payload interface{}) {
	data, err := json.Marshal(WSMessage{Type: "update", Data: payload})
	if err != nil {
		return
	}

	h.mu.RLock()
	defer h.mu.RUnlock()

	for client := range h.clients {
		if client.Filter[filterKey] == filterValue || client.Filter[filterKey] == "" {
			select {
			case client.Send <- data:
			default:
			}
		}
	}
}

// WSMessage is the WebSocket message format
type WSMessage struct {
	Type string      `json:"type"` // "update", "error", "ping", "initial"
	Data interface{} `json:"data"`
}
