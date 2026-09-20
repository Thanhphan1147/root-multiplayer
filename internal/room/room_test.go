package room

import (
	"testing"

	"github.com/Thanhphan1147/root-mn/pkg/root"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	return &Store{Dir: t.TempDir(), Secret: []byte("test-secret")}
}

func TestCreateAndTokens(t *testing.T) {
	s := newStore(t)
	rm, tokens, err := s.Create(4, []string{"a", "b", "c", "d"})
	if err != nil {
		t.Fatal(err)
	}
	if len(tokens) != 4 || len(rm.Seats) != 4 {
		t.Fatalf("tokens=%d seats=%d", len(tokens), len(rm.Seats))
	}
	for i, tok := range tokens {
		c, err := VerifyToken(s.Secret, tok)
		if err != nil {
			t.Fatalf("token %d: %v", i, err)
		}
		if c.Room != rm.ID || c.Seat != i {
			t.Fatalf("claims = %+v", c)
		}
	}
	if _, _, err := s.Create(5, nil); err == nil {
		t.Fatal("5 players should be rejected")
	}
}

func TestPickFactionsStartsGame(t *testing.T) {
	s := newStore(t)
	rm, _, _ := s.Create(4, nil)
	for i, f := range []string{"MC", "ED", "WA", "VB"} {
		room, g, err := s.PickFaction(rm.ID, i, f)
		if err != nil {
			t.Fatalf("pick %d %s: %v", i, f, err)
		}
		if i < 3 && room.Started {
			t.Fatal("game started too early")
		}
		if i == 3 {
			if !room.Started || g == nil {
				t.Fatal("game should have started")
			}
			if !g.SetupMode {
				t.Fatal("engine should be in setup mode")
			}
			if room.First != "MC" {
				t.Fatalf("first = %s, want MC", room.First)
			}
		}
	}
	// Duplicate faction is rejected.
	if _, _, err := s.PickFaction(rm.ID, 0, "ED"); err == nil {
		t.Fatal("duplicate faction should be rejected")
	}
}

func TestTurnEnforcement(t *testing.T) {
	s := newStore(t)
	rm, _, _ := s.Create(4, nil)
	for i, f := range []string{"MC", "ED", "WA", "VB"} {
		if _, _, err := s.PickFaction(rm.ID, i, f); err != nil {
			t.Fatal(err)
		}
	}
	// MC is first, so seat 0 may act and seat 1 may not.
	_, g, _ := s.Load(rm.ID)
	legal := g.LegalActions()
	if len(legal) == 0 {
		t.Fatal("expected a legal setup action")
	}
	if _, _, err := s.ApplyAction(rm.ID, 1, legal[0].ID); err == nil {
		t.Fatal("seat 1 should not be allowed to act on MC's setup")
	}
	if _, _, err := s.ApplyAction(rm.ID, 0, legal[0].ID); err != nil {
		t.Fatalf("seat 0 action failed: %v", err)
	}
}

func TestRedaction(t *testing.T) {
	s := newStore(t)
	rm, _, _ := s.Create(4, nil)
	for i, f := range []string{"MC", "ED", "WA", "VB"} {
		if _, _, err := s.PickFaction(rm.ID, i, f); err != nil {
			t.Fatal(err)
		}
	}
	_, g, _ := s.Load(rm.ID)
	// Finish setup so hands exist (cards are drawn at the end of setup).
	for g.SetupMode {
		acts := g.LegalActions()
		if len(acts) == 0 {
			t.Fatal("setup stalled")
		}
		if err := g.Apply(acts[0]); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}
	snap := Redact(g, "MC")
	players := snap["players"].(map[root.Faction]*root.Player)
	if got := players["MC"].Hand; len(got) == 0 || got[0] == "??" {
		t.Fatalf("viewer's own hand should be visible: %v", got)
	}
	if got := players["ED"].Hand; len(got) == 0 || got[0] != "??" {
		t.Fatalf("other hand should be masked: %v", got)
	}
	if snap["hash"] != "" {
		t.Fatalf("hash should be blank, got %v", snap["hash"])
	}
}
