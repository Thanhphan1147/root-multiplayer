// Package httpapi exposes the correspondence-play HTTP API and static client.
package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/Thanhphan1147/root-mn/pkg/root"
	"github.com/Thanhphan1147/root-multiplayer/internal/room"
)

// Server wires the room store to HTTP.
type Server struct {
	Store  *room.Store
	WebDir string
	hubv   *Hub
}

// hub returns the WebSocket notification hub, creating it on first use.
func (s *Server) hub() *Hub {
	if s.hubv == nil {
		s.hubv = NewHub()
	}
	return s.hubv
}

// Handler builds the mux.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", s.health)
	mux.HandleFunc("/api/rooms", s.rooms)
	mux.HandleFunc("/api/take", s.take)
	mux.HandleFunc("/api/leave", s.leave)
	mux.HandleFunc("/api/state", s.state)
	mux.HandleFunc("/api/faction", s.faction)
	mux.HandleFunc("/api/action", s.action)
	mux.HandleFunc("/api/export", s.export)
	mux.HandleFunc("/api/ws", s.ws)
	if s.WebDir != "" {
		mux.Handle("/", noCache(http.FileServer(http.Dir(s.WebDir))))
	}
	return withCORS(withRecover(mux))
}

// withRecover turns a handler panic into a 500 so the client gets a clear error
// instead of a dropped connection.
func withRecover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("panic serving %s %s: %v", r.Method, r.URL.Path, rec)
				writeError(w, http.StatusInternalServerError, "internal error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// noCache forces browsers to revalidate static assets so a redeploy is picked
// up on a normal reload instead of a stale cached copy.
func noCache(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		next.ServeHTTP(w, r)
	})
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, If-None-Match")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Expose-Headers", "ETag")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// rooms lists the administrator's rooms for the lobby.
func (s *Server) rooms(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "GET only")
		return
	}
	list, err := s.Store.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list rooms")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"rooms": list})
}

// take occupies a free seat and returns its token.
func (s *Server) take(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST only")
		return
	}
	var req struct {
		Room string `json:"room"`
		Seat string `json:"seat"`
	}
	if err := decode(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(req.Room) == "" || strings.TrimSpace(req.Seat) == "" {
		writeError(w, http.StatusBadRequest, "missing room or seat")
		return
	}
	rm, _, seat, tok, err := s.Store.Take(req.Room, req.Seat)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"room": rm, "seat": seat, "token": tok})
}

// leave frees the caller's seat and rotates its token.
func (s *Server) leave(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST only")
		return
	}
	rm, seat, g, ok := s.auth(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing or invalid token")
		return
	}
	if _, _, err := s.Store.Leave(rm.ID, seat.ID); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	s.broadcast(rm.ID, g)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) state(w http.ResponseWriter, r *http.Request) {
	rm, seat, g, ok := s.auth(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing or invalid token")
		return
	}
	payload := s.payload(rm, g, seat.Index)
	etag := etagOf(payload)
	w.Header().Set("ETag", etag)
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	writeJSON(w, http.StatusOK, payload)
}

func (s *Server) faction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST only")
		return
	}
	rm, seat, _, ok := s.auth(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing or invalid token")
		return
	}
	var req struct {
		Faction string `json:"faction"`
	}
	if err := decode(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	updated, g, err := s.Store.PickFaction(rm.ID, seat.ID, req.Faction)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	s.broadcast(rm.ID, g)
	s.respondState(w, s.payload(updated, g, seat.Index))
}

func (s *Server) action(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST only")
		return
	}
	rm, seat, _, ok := s.auth(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing or invalid token")
		return
	}
	var req struct {
		ID string `json:"id"`
	}
	if err := decode(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	updated, g, err := s.Store.ApplyAction(rm.ID, seat.ID, req.ID)
	if err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	s.broadcast(rm.ID, g)
	s.respondState(w, s.payload(updated, g, seat.Index))
}

func (s *Server) export(w http.ResponseWriter, r *http.Request) {
	rm, _, _, ok := s.auth(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing or invalid token")
		return
	}
	text, err := s.Store.Export(rm.ID)
	if err != nil {
		writeError(w, http.StatusNotFound, "room not found")
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+rm.ID+`.rmn"`)
	_, _ = w.Write([]byte(text))
}

// broadcast nudges live clients that the room changed (seq included so they can
// skip a fetch if they already have it).
func (s *Server) broadcast(roomID string, g *root.Game) {
	if g == nil {
		return
	}
	s.hub().Broadcast(roomID, map[string]any{"type": "changed", "room": roomID, "seq": g.Seq})
}

func (s *Server) respondState(w http.ResponseWriter, payload map[string]any) {
	w.Header().Set("ETag", etagOf(payload))
	writeJSON(w, http.StatusOK, payload)
}

func (s *Server) payload(rm *room.Room, g *root.Game, seat int) map[string]any {
	viewer := seatFaction(rm, seat)
	p := map[string]any{"room": rm, "seat": seat, "you": viewer}
	if g != nil {
		for k, v := range room.Redact(g, viewer) {
			p[k] = v
		}
		p["seq"] = g.Seq
	}
	return p
}

// auth resolves the seat token on a request to a room, seat, and game.
func (s *Server) auth(r *http.Request) (*room.Room, room.Seat, *root.Game, bool) {
	tok := ""
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		tok = strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
	}
	if tok == "" {
		tok = r.URL.Query().Get("token")
	}
	if tok == "" {
		return nil, room.Seat{}, nil, false
	}
	rm, seat, g, err := s.Store.Auth(tok)
	if err != nil {
		return nil, room.Seat{}, nil, false
	}
	return rm, seat, g, true
}

func seatFaction(rm *room.Room, seat int) string {
	if seat < 0 || seat >= len(rm.Seats) {
		return ""
	}
	return rm.Seats[seat].Faction
}

func etagOf(v any) string {
	b, _ := json.Marshal(v)
	sum := sha256.Sum256(b)
	return `"` + hex.EncodeToString(sum[:8]) + `"`
}

func decode(r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	return dec.Decode(v)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"error": msg})
}
