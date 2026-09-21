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
	rm, tokens, err := s.Create(4, []string{"a", "b", "c", "d"}, true)
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
	if _, _, err := s.Create(5, nil, true); err == nil {
		t.Fatal("5 players should be rejected")
	}
}

func TestPickFactionsStartsGame(t *testing.T) {
	s := newStore(t)
	rm, _, _ := s.Create(4, nil, true)
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
	rm, _, _ := s.Create(4, nil, true)
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
	rm, _, _ := s.Create(4, nil, true)
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

func TestTwoPlayerSetupCompletes(t *testing.T) {
	s := newStore(t)
	rm, _, _ := s.Create(2, nil, true)
	if _, _, err := s.PickFaction(rm.ID, 0, "MC"); err != nil {
		t.Fatal(err)
	}
	started, g, err := s.PickFaction(rm.ID, 1, "ED")
	if err != nil {
		t.Fatal(err)
	}
	if !started.Started {
		t.Fatal("game should have started")
	}
	// Drive setup through the store; this used to panic when the Eyrie chose a
	// leader because the setup machine assumed the Alliance was present.
	for g.SetupMode {
		acts := g.LegalActions()
		if len(acts) == 0 {
			t.Fatalf("stuck at stage %s", g.SetupStage)
		}
		seat := 0
		for i, st := range started.Seats {
			if st.Faction == string(g.Current) {
				seat = i
			}
		}
		if _, _, err := s.ApplyAction(rm.ID, seat, acts[0].ID); err != nil {
			t.Fatalf("apply %s: %v", acts[0].ID, err)
		}
		_, g, _ = s.Load(rm.ID)
	}
}

func TestClaimNextSeat(t *testing.T) {
	s := newStore(t)
	rm, _, err := s.Create(4, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if !rm.Seats[0].Claimed {
		t.Fatal("the creator's seat should be reserved")
	}
	for i, want := range []int{1, 2, 3} {
		r, seat, tok, err := s.ClaimNext(rm.ID)
		if err != nil {
			t.Fatalf("claim %d: %v", i, err)
		}
		if seat != want {
			t.Fatalf("claimed seat %d, want %d", seat, want)
		}
		if !r.Seats[seat].Claimed {
			t.Fatalf("seat %d should be marked claimed", seat)
		}
		c, err := VerifyToken(s.Secret, tok)
		if err != nil || c.Room != rm.ID || c.Seat != seat {
			t.Fatalf("bad token for seat %d: %v", seat, err)
		}
	}
	if _, _, _, err := s.ClaimNext(rm.ID); err == nil {
		t.Fatal("a full room should reject the join")
	}
}

func TestPrivateRoomIsInviteOnly(t *testing.T) {
	s := newStore(t)
	rm, _, err := s.Create(3, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := s.ClaimNext(rm.ID); err == nil {
		t.Fatal("invite-only rooms should not be joinable")
	}
	list, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("invite-only room should not be listed: %+v", list)
	}
}

func TestListOpenRooms(t *testing.T) {
	s := newStore(t)
	open, _, err := s.Create(3, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Create(3, nil, false); err != nil {
		t.Fatal(err)
	}
	list, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != open.ID {
		t.Fatalf("list = %+v", list)
	}
	if list[0].OpenSeats != 2 {
		t.Fatalf("openSeats = %d, want 2", list[0].OpenSeats)
	}
	for i := 0; i < 2; i++ {
		if _, _, _, err := s.ClaimNext(open.ID); err != nil {
			t.Fatal(err)
		}
	}
	if list, _ = s.List(); len(list) != 0 {
		t.Fatalf("full room should not be listed: %+v", list)
	}
}

func TestPendingPlayerActsDuringBattle(t *testing.T) {
	s, rm, g, _ := startedBattle(t)
	var hit *root.Action
	acts := g.LegalActions()
	for i := range acts {
		if acts[i].Kind == "battle-hit" {
			hit = &acts[i]
			break
		}
	}
	if hit == nil {
		t.Fatal("no battle-hit action available")
	}
	if _, _, err := s.ApplyAction(rm.ID, 0, hit.ID); err == nil {
		t.Fatal("the turn player should not act on the defender's pending hits")
	}
	if _, _, err := s.ApplyAction(rm.ID, 1, hit.ID); err != nil {
		t.Fatalf("the pending player should be allowed to act: %v", err)
	}
}

func TestPendingChoiceRedaction(t *testing.T) {
	_, _, g, _ := startedBattle(t)
	// ED is the pending player while MC owns the turn.
	ed := Redact(g, "ED")
	if acts, _ := ed["legal"].([]root.Action); len(acts) == 0 {
		t.Fatal("the pending player should receive legal actions")
	}
	if p, _ := ed["pending"].(*root.Pending); p == nil || p.Player != root.ED {
		t.Fatalf("the pending player should see the pending choice: %v", ed["pending"])
	}
	mc := Redact(g, "MC")
	if acts, _ := mc["legal"].([]root.Action); len(acts) != 0 {
		t.Fatalf("a non-acting player should get no legal actions, got %v", acts)
	}
	if p, _ := mc["pending"].(*root.Pending); p == nil || p.Player != root.ED {
		t.Fatalf("everyone should see whose choice it is: %v", mc["pending"])
	}
}

// startedBattle returns a started 2-player MC-vs-ED game forced into a battle
// where MC owns the turn but ED must assign the hits.
func startedBattle(t *testing.T) (*Store, *Room, *root.Game, map[string]int) {
	t.Helper()
	s := newStore(t)
	rm, _, err := s.Create(2, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.PickFaction(rm.ID, 0, "MC"); err != nil {
		t.Fatal(err)
	}
	started, g, err := s.PickFaction(rm.ID, 1, "ED")
	if err != nil {
		t.Fatal(err)
	}
	seatOf := map[string]int{}
	for i, st := range started.Seats {
		seatOf[st.Faction] = i
	}
	for g.SetupMode {
		acts := g.LegalActions()
		if len(acts) == 0 {
			t.Fatal("setup stalled")
		}
		if _, _, err := s.ApplyAction(rm.ID, seatOf[string(g.Current)], acts[0].ID); err != nil {
			t.Fatalf("setup %s: %v", acts[0].ID, err)
		}
		_, g, _ = s.Load(rm.ID)
	}
	g.Current = root.MC
	g.Clearings["C1"].Warriors[root.MC] = 3
	g.Clearings["C1"].Warriors[root.ED] = 3
	g.Battle = &root.BattleState{
		Clearing: "C1", Attacker: root.MC, Defender: root.ED,
		Stage: root.StageHits, HitSide: root.ED, Remaining: 1, AtkHits: 1,
	}
	g.Pending = &root.Pending{Kind: root.PendingBattleHits, Player: root.ED}
	if err := s.Save(started, g); err != nil {
		t.Fatal(err)
	}
	return s, started, g, seatOf
}
