package httpapi

import (
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Hub tracks live WebSocket connections per room and nudges them when the room
// changes. It is a notification channel only: clients still fetch state over
// HTTP (with ETag), so redaction and caching stay in one place.
type Hub struct {
	mu    sync.Mutex
	rooms map[string]map[*wsClient]bool
}

// NewHub creates an empty hub.
func NewHub() *Hub {
	return &Hub{rooms: map[string]map[*wsClient]bool{}}
}

func (h *Hub) add(roomID string, c *wsClient) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.rooms[roomID] == nil {
		h.rooms[roomID] = map[*wsClient]bool{}
	}
	h.rooms[roomID][c] = true
}

func (h *Hub) remove(roomID string, c *wsClient) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if set := h.rooms[roomID]; set != nil {
		delete(set, c)
		if len(set) == 0 {
			delete(h.rooms, roomID)
		}
	}
}

// Broadcast notifies every connection in a room.
func (h *Hub) Broadcast(roomID string, msg any) {
	h.mu.Lock()
	clients := make([]*wsClient, 0, len(h.rooms[roomID]))
	for c := range h.rooms[roomID] {
		clients = append(clients, c)
	}
	h.mu.Unlock()
	for _, c := range clients {
		_ = c.write(msg)
	}
}

type wsClient struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

func (c *wsClient) write(v any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	_ = c.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	return c.conn.WriteJSON(v)
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	// The client is normally same-origin; allow others (token still required).
	CheckOrigin: func(r *http.Request) bool { return true },
}

func (s *Server) ws(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	// Auth: prefer the first frame (keeps the token out of URLs/logs); fall
	// back to ?token= for simple clients.
	tok := r.URL.Query().Get("token")
	if tok == "" {
		_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
		var m struct {
			Token string `json:"token"`
		}
		if err := conn.ReadJSON(&m); err != nil {
			return
		}
		tok = m.Token
	}
	rm, _, _, err := s.Store.Auth(tok)
	if err != nil {
		_ = conn.WriteJSON(map[string]any{"type": "error", "error": "unauthorized"})
		return
	}
	_ = conn.SetReadDeadline(time.Time{})

	client := &wsClient{conn: conn}
	s.hub().add(rm.ID, client)
	defer s.hub().remove(rm.ID, client)

	_ = client.write(map[string]any{"type": "ready", "room": rm.ID})

	// Keepalive pings, and read/discard until the client goes away.
	conn.SetPongHandler(func(string) error { return conn.SetReadDeadline(time.Now().Add(60 * time.Second)) })
	go func() {
		t := time.NewTicker(25 * time.Second)
		defer t.Stop()
		for range t.C {
			client.mu.Lock()
			_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			err := conn.WriteMessage(websocket.PingMessage, nil)
			client.mu.Unlock()
			if err != nil {
				return
			}
		}
	}()
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
	}
}
