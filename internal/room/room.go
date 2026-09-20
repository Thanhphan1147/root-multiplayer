package room

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Thanhphan1147/root-mn/pkg/root"
)

// Seat is one player slot in a room.
type Seat struct {
	Index   int    `json:"index"`
	Name    string `json:"name,omitempty"`
	Faction string `json:"faction,omitempty"` // "", or one of C/ED/WA/VB
}

// Room is the persistent room metadata.
type Room struct {
	ID      string    `json:"id"`
	Created time.Time `json:"created"`
	Players int       `json:"players"`
	Seats   []Seat    `json:"seats"`
	Started bool      `json:"started"`
	First   string    `json:"first,omitempty"`
}

// Store persists rooms on disk.
type Store struct {
	Dir    string
	Secret []byte
	mu     sync.Mutex
}

func (s *Store) roomDir(id string) string   { return filepath.Join(s.Dir, "rooms", id) }
func (s *Store) roomPath(id string) string  { return filepath.Join(s.roomDir(id), "room.json") }
func (s *Store) statePath(id string) string { return filepath.Join(s.roomDir(id), "state.json") }
func (s *Store) rmnPath(id string) string   { return filepath.Join(s.roomDir(id), "game.rmn") }

// Create makes a new room and returns it plus one token per seat.
func (s *Store) Create(players int, names []string) (*Room, []string, error) {
	if players < 2 || players > 4 {
		return nil, nil, errors.New("players must be between 2 and 4")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	room := &Room{ID: randomID(4), Created: time.Now().UTC(), Players: players}
	tokens := make([]string, players)
	for i := 0; i < players; i++ {
		seat := Seat{Index: i}
		if i < len(names) {
			seat.Name = strings.TrimSpace(names[i])
		}
		room.Seats = append(room.Seats, seat)
		tokens[i] = IssueToken(s.Secret, room.ID, i, 0)
	}
	if err := os.MkdirAll(s.roomDir(room.ID), 0o755); err != nil {
		return nil, nil, err
	}
	if err := s.saveRoom(room); err != nil {
		return nil, nil, err
	}
	return room, tokens, nil
}

// Load reads a room and, if started, its game state.
func (s *Store) Load(id string) (*Room, *root.Game, error) {
	room := &Room{}
	if err := readJSON(s.roomPath(id), room); err != nil {
		return nil, nil, err
	}
	if !room.Started {
		return room, nil, nil
	}
	g := &root.Game{}
	if err := readJSON(s.statePath(id), g); err != nil {
		return room, nil, err
	}
	return room, g, nil
}

// PickFaction assigns a faction to a seat (lobby only) and starts the game once
// every seat has chosen.
func (s *Store) PickFaction(id string, seat int, faction string) (*Room, *root.Game, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	room, g, err := s.Load(id)
	if err != nil {
		return nil, nil, err
	}
	if room.Started {
		return room, g, errors.New("the game has already started")
	}
	if seat < 0 || seat >= len(room.Seats) {
		return room, nil, errors.New("invalid seat")
	}
	f := root.Faction(strings.ToUpper(faction))
	if !f.IsBase() {
		return room, nil, errors.New("unknown faction")
	}
	if room.Seats[seat].Faction != "" {
		return room, nil, errors.New("this seat already picked a faction")
	}
	for _, st := range room.Seats {
		if st.Faction == string(f) {
			return room, nil, errors.New("that faction is already taken")
		}
	}
	room.Seats[seat].Faction = string(f)

	if allPicked(room) {
		factions := make([]root.Faction, 0, len(room.Seats))
		for _, st := range room.Seats {
			factions = append(factions, root.Faction(st.Faction))
		}
		first := factions[0]
		for _, fa := range factions {
			if fa == root.MC {
				first = fa // the Marquise traditionally starts
				break
			}
		}
		var seedb [8]byte
		_, _ = rand.Read(seedb[:])
		g = root.NewGame(factions, first, binary.LittleEndian.Uint64(seedb[:]))
		root.BeginSetup(g)
		room.Started = true
		room.First = string(first)
	}
	if err := s.Save(room, g); err != nil {
		return room, g, err
	}
	return room, g, nil
}

// ApplyAction validates that it is the seat's turn and applies an action.
func (s *Store) ApplyAction(id string, seat int, actionID string) (*Room, *root.Game, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	room, g, err := s.Load(id)
	if err != nil {
		return nil, nil, err
	}
	if !room.Started || g == nil {
		return room, nil, errors.New("the game has not started")
	}
	if seat < 0 || seat >= len(room.Seats) {
		return room, g, errors.New("invalid seat")
	}
	if room.Seats[seat].Faction != string(g.Current) {
		return room, g, fmt.Errorf("it is not your turn")
	}
	if err := g.Apply(root.Action{ID: actionID}); err != nil {
		return room, g, err
	}
	if err := s.Save(room, g); err != nil {
		return room, g, err
	}
	return room, g, nil
}

// Save writes room metadata, the authoritative state, and the .rmn log.
func (s *Store) Save(room *Room, g *root.Game) error {
	if err := os.MkdirAll(s.roomDir(room.ID), 0o755); err != nil {
		return err
	}
	if err := writeJSON(s.roomPath(room.ID), room); err != nil {
		return err
	}
	if g == nil {
		return nil
	}
	if err := writeJSON(s.statePath(room.ID), g); err != nil {
		return err
	}
	return os.WriteFile(s.rmnPath(room.ID), []byte(s.RMN(room, g)), 0o644)
}

// RMN renders the canonical header + the engine's RMN event log.
func (s *Store) RMN(room *Room, g *root.Game) string {
	var b strings.Builder
	b.WriteString("%RMN 3.0\n")
	fmt.Fprintf(&b, "%%Game %s\n", room.ID)
	b.WriteString("%Map autumn\n%Deck standard\n")
	for _, st := range room.Seats {
		if st.Faction == "" {
			continue
		}
		f := root.Faction(st.Faction)
		fmt.Fprintf(&b, "%%Faction %s %s seat=%d", f, f.Kind(), st.Index+1)
		if st.Name != "" {
			fmt.Fprintf(&b, " name=%q", st.Name)
		}
		b.WriteByte('\n')
	}
	if room.First != "" {
		fmt.Fprintf(&b, "%%First %s\n", room.First)
	}
	for _, line := range g.RMNLog {
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

// Export returns the .rmn log for a room.
func (s *Store) Export(id string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	room, g, err := s.Load(id)
	if err != nil {
		return "", err
	}
	return s.RMN(room, g), nil
}

func allPicked(room *Room) bool {
	for _, st := range room.Seats {
		if st.Faction == "" {
			return false
		}
	}
	return true
}

func (s *Store) saveRoom(room *Room) error {
	return writeJSON(s.roomPath(room.ID), room)
}

func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
