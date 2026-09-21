package room

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Thanhphan1147/root-mn/pkg/root"
)

// Seat is one player slot in a room. ID is stable for the room's lifetime, so a
// token keeps pointing at the same seat even after other seats are removed.
type Seat struct {
	ID       string `json:"id"`
	Index    int    `json:"index"` // display position (0-based); renumbered on removal
	Name     string `json:"name,omitempty"`
	Faction  string `json:"faction,omitempty"`
	Occupied bool   `json:"occupied,omitempty"`
	Version  int    `json:"version,omitempty"` // bumped to revoke this seat's token
}

// Room is the persistent room metadata. Rooms are created by the administrator,
// who then arranges seats for the players.
type Room struct {
	ID      string    `json:"id"`
	Name    string    `json:"name,omitempty"`
	Created time.Time `json:"created"`
	Seats   []Seat    `json:"seats"`
	Started bool      `json:"started"`
	First   string    `json:"first,omitempty"`
}

// Summary is the public, token-free view of a room for the lobby.
type Summary struct {
	ID      string    `json:"id"`
	Name    string    `json:"name,omitempty"`
	Created time.Time `json:"created"`
	Started bool      `json:"started"`
	Seats   []Seat    `json:"seats"`
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

// Create makes an empty room with the given number of seats. It is the
// administrator's entry point; players take seats from the browser.
func (s *Store) Create(name string, seats int) (*Room, error) {
	if seats < 2 || seats > 4 {
		return nil, errors.New("seats must be between 2 and 4")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	room := &Room{ID: randomID(4), Name: strings.TrimSpace(name), Created: time.Now().UTC()}
	for i := 0; i < seats; i++ {
		room.Seats = append(room.Seats, Seat{ID: randomID(4), Index: i})
	}
	if err := os.MkdirAll(s.roomDir(room.ID), 0o755); err != nil {
		return nil, err
	}
	if err := s.saveRoom(room); err != nil {
		return nil, err
	}
	return room, nil
}

// List returns every room, newest first.
func (s *Store) List() ([]Summary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(filepath.Join(s.Dir, "rooms"))
	if err != nil {
		if os.IsNotExist(err) {
			return []Summary{}, nil
		}
		return nil, err
	}
	out := []Summary{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		room := &Room{}
		if err := readJSON(s.roomPath(e.Name()), room); err != nil {
			continue
		}
		// Skip legacy rooms from before stable seat ids existed.
		if len(room.Seats) == 0 || room.Seats[0].ID == "" {
			continue
		}
		out = append(out, Summary{
			ID: room.ID, Name: room.Name, Created: room.Created,
			Started: room.Started, Seats: room.Seats,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Created.After(out[j].Created) })
	return out, nil
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

// seatIndex returns the position of a seat by id, or -1.
func (r *Room) seatIndex(id string) int {
	for i := range r.Seats {
		if r.Seats[i].ID == id {
			return i
		}
	}
	return -1
}

// renumber makes seat Index values match their slice position.
func (r *Room) renumber() {
	for i := range r.Seats {
		r.Seats[i].Index = i
	}
}

// Auth verifies a token and returns the room, seat, and (if started) game it
// belongs to. It rejects tokens for seats that have since been freed or removed.
func (s *Store) Auth(token string) (*Room, Seat, *root.Game, error) {
	c, err := VerifyToken(s.Secret, token)
	if err != nil {
		return nil, Seat{}, nil, err
	}
	room, g, err := s.Load(c.Room)
	if err != nil {
		return nil, Seat{}, nil, err
	}
	i := room.seatIndex(c.Seat)
	if i < 0 || room.Seats[i].Version != c.Ver {
		return nil, Seat{}, nil, errors.New("this seat token is no longer valid")
	}
	return room, room.Seats[i], g, nil
}

// Take occupies a free seat and returns a token for it.
func (s *Store) Take(id, seatID string) (*Room, *root.Game, int, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	room, g, err := s.Load(id)
	if err != nil {
		return nil, nil, -1, "", err
	}
	i := room.seatIndex(seatID)
	if i < 0 {
		return room, g, -1, "", errors.New("no such seat")
	}
	if room.Seats[i].Occupied {
		return room, g, -1, "", errors.New("that seat is already taken")
	}
	room.Seats[i].Occupied = true
	if err := s.Save(room, g); err != nil {
		return room, g, -1, "", err
	}
	tok := IssueToken(s.Secret, room.ID, seatID, room.Seats[i].Version, 0)
	return room, g, i, tok, nil
}

// Leave frees the caller's seat and rotates its token. Before the game starts
// the seat's faction is released too; after it starts the faction stays in play
// for whoever takes the seat next.
func (s *Store) Leave(id, seatID string) (*Room, *root.Game, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	room, g, err := s.Load(id)
	if err != nil {
		return nil, nil, err
	}
	i := room.seatIndex(seatID)
	if i < 0 {
		return room, g, errors.New("no such seat")
	}
	room.Seats[i].Occupied = false
	room.Seats[i].Version++
	if !room.Started {
		room.Seats[i].Faction = ""
	}
	if err := s.Save(room, g); err != nil {
		return room, g, err
	}
	return room, g, nil
}

// Kick removes a seat and, once the game has started, drops its faction from
// the game entirely. Remaining seats are renumbered. Administrator only.
func (s *Store) Kick(id, seatID string) (*Room, *root.Game, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	room, g, err := s.Load(id)
	if err != nil {
		return nil, nil, err
	}
	i := room.seatIndex(seatID)
	if i < 0 {
		return room, g, errors.New("no such seat")
	}
	faction := root.Faction(room.Seats[i].Faction)
	room.Seats = append(room.Seats[:i:i], room.Seats[i+1:]...)
	room.renumber()
	if room.Started && g != nil && faction != "" {
		g.RemoveFaction(faction)
	}
	if err := s.Save(room, g); err != nil {
		return room, g, err
	}
	return room, g, nil
}

// PickFaction assigns a faction to a seat (lobby only) and starts the game once
// every seat has chosen.
func (s *Store) PickFaction(id, seatID, faction string) (*Room, *root.Game, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	room, g, err := s.Load(id)
	if err != nil {
		return nil, nil, err
	}
	if room.Started {
		return room, g, errors.New("the game has already started")
	}
	i := room.seatIndex(seatID)
	if i < 0 {
		return room, nil, errors.New("no such seat")
	}
	if !room.Seats[i].Occupied {
		return room, nil, errors.New("that seat is not taken")
	}
	f := root.Faction(strings.ToUpper(faction))
	if !f.IsBase() {
		return room, nil, errors.New("unknown faction")
	}
	if room.Seats[i].Faction != "" {
		return room, nil, errors.New("this seat already picked a faction")
	}
	for _, st := range room.Seats {
		if st.Faction == string(f) {
			return room, nil, errors.New("that faction is already taken")
		}
	}
	room.Seats[i].Faction = string(f)

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
func (s *Store) ApplyAction(id, seatID, actionID string) (*Room, *root.Game, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	room, g, err := s.Load(id)
	if err != nil {
		return nil, nil, err
	}
	if !room.Started || g == nil {
		return room, nil, errors.New("the game has not started")
	}
	i := room.seatIndex(seatID)
	if i < 0 {
		return room, g, errors.New("no such seat")
	}
	// Pending choices (battle hits, discards, field hospitals) belong to the
	// player the engine is waiting on, which is not always g.Current.
	actor := g.Current
	if g.Pending != nil {
		actor = g.Pending.Player
	}
	if room.Seats[i].Faction != string(actor) {
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
	if len(room.Seats) == 0 {
		return false
	}
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
