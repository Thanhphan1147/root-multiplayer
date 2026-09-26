package room

import (
	"testing"

	"github.com/Thanhphan1147/root-mn/pkg/root"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	return &Store{Dir: t.TempDir(), Secret: []byte("test-secret")}
}

// takeN takes the first n seats of a table and returns their ids and tokens.
func takeN(t *testing.T, s *Store, roomID string, n int) (ids, tokens []string) {
	t.Helper()
	rm, _, err := s.Load(roomID)
	if err != nil {
		t.Fatal(err)
	}
	if n > len(rm.Seats) {
		t.Fatalf("take %d seats from a %d-seat table", n, len(rm.Seats))
	}
	for _, st := range rm.Seats[:n] {
		_, _, _, tok, err := s.Take(roomID, st.ID)
		if err != nil {
			t.Fatalf("take seat %s: %v", st.ID, err)
		}
		ids = append(ids, st.ID)
		tokens = append(tokens, tok)
	}
	return ids, tokens
}

func TestCreateAndTakeSeat(t *testing.T) {
	s := newStore(t)
	rm, err := s.Create("Test")
	if err != nil {
		t.Fatal(err)
	}
	if len(rm.Seats) != 4 {
		t.Fatalf("a table should have 4 seats, got %d", len(rm.Seats))
	}
	ids, tokens := takeN(t, s, rm.ID, 4)
	for i, tok := range tokens {
		_, seat, _, err := s.Auth(tok)
		if err != nil {
			t.Fatalf("token %d: %v", i, err)
		}
		if seat.ID != ids[i] || seat.Index != i {
			t.Fatalf("token %d -> %+v, want id %s index %d", i, seat, ids[i], i)
		}
	}
	if _, _, _, _, err := s.Take(rm.ID, ids[0]); err == nil {
		t.Fatal("an occupied seat should not be takeable")
	}
}

func TestLeaveRotatesToken(t *testing.T) {
	s := newStore(t)
	rm, _ := s.Create("")
	ids, tokens := takeN(t, s, rm.ID, 2)

	if _, _, err := s.Leave(rm.ID, ids[0]); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := s.Auth(tokens[0]); err == nil {
		t.Fatal("the old token should be rejected after leaving")
	}
	_, _, _, tok2, err := s.Take(rm.ID, ids[0])
	if err != nil {
		t.Fatalf("re-taking the freed seat: %v", err)
	}
	if tok2 == tokens[0] {
		t.Fatal("a fresh token should be issued")
	}
	if _, _, _, err := s.Auth(tok2); err != nil {
		t.Fatalf("the new token should work: %v", err)
	}
}

func TestKickDropsFaction(t *testing.T) {
	s := newStore(t)
	rm, _ := s.Create("")
	ids, tokens := takeN(t, s, rm.ID, 4)
	for i, f := range []string{"MC", "ED", "WA", "VB"} {
		if _, _, err := s.PickFaction(rm.ID, ids[i], f); err != nil {
			t.Fatalf("pick %s: %v", f, err)
		}
	}
	updated, g, err := s.Kick(rm.ID, ids[1]) // remove ED
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.Seats) != 4 {
		t.Fatalf("the table should keep 4 seats, got %d", len(updated.Seats))
	}
	for i, st := range updated.Seats {
		if st.Index != i {
			t.Fatalf("seat index drift: %+v", updated.Seats)
		}
	}
	if kicked := updated.Seats[1]; kicked.Occupied || kicked.Faction != "" {
		t.Fatalf("the kicked chair should be empty: %+v", kicked)
	}
	if _, ok := g.Players[root.ED]; ok {
		t.Fatal("ED should be dropped from the game")
	}
	if _, _, _, err := s.Auth(tokens[1]); err == nil {
		t.Fatal("the kicked seat's token should be rejected")
	}
}

func TestStartWithTwoOfFour(t *testing.T) {
	s := newStore(t)
	rm, _ := s.Create("")
	ids, _ := takeN(t, s, rm.ID, 2)
	if _, _, err := s.PickFaction(rm.ID, ids[0], "MC"); err != nil {
		t.Fatal(err)
	}
	started, g, err := s.PickFaction(rm.ID, ids[1], "ED")
	if err != nil {
		t.Fatal(err)
	}
	if !started.Started || g == nil {
		t.Fatal("a two-player game should start")
	}
	if len(started.Seats) != 4 {
		t.Fatalf("the table should keep 4 seats, got %d", len(started.Seats))
	}
	free := started.Seats[2]
	if free.Occupied || free.Faction != "" {
		t.Fatalf("an unused chair should stay empty: %+v", free)
	}
	if _, _, _, _, err := s.Take(rm.ID, free.ID); err == nil {
		t.Fatal("an unused chair should not be takeable after the game starts")
	}
}

func TestPickFactionsStartsGame(t *testing.T) {
	s := newStore(t)
	rm, _ := s.Create("")
	ids, _ := takeN(t, s, rm.ID, 4)
	for i, f := range []string{"MC", "ED", "WA", "VB"} {
		room, g, err := s.PickFaction(rm.ID, ids[i], f)
		if err != nil {
			t.Fatalf("pick %s: %v", f, err)
		}
		if i < 3 && room.Started {
			t.Fatal("the game started too early")
		}
		if i == 3 {
			if !room.Started || g == nil {
				t.Fatal("the game should have started")
			}
			if !g.SetupMode {
				t.Fatal("the engine should be in setup mode")
			}
			if room.First != "MC" {
				t.Fatalf("first = %s, want MC", room.First)
			}
		}
	}
}

func TestTurnEnforcement(t *testing.T) {
	s := newStore(t)
	rm, _ := s.Create("")
	ids, _ := takeN(t, s, rm.ID, 2)
	if _, _, err := s.PickFaction(rm.ID, ids[0], "MC"); err != nil {
		t.Fatal(err)
	}
	_, g, err := s.PickFaction(rm.ID, ids[1], "ED")
	if err != nil {
		t.Fatal(err)
	}
	legal := g.LegalActions()
	if len(legal) == 0 {
		t.Fatal("expected a legal setup action")
	}
	if _, _, err := s.ApplyAction(rm.ID, ids[1], legal[0].ID); err == nil {
		t.Fatal("seat 1 should not act on MC's setup")
	}
	if _, _, err := s.ApplyAction(rm.ID, ids[0], legal[0].ID); err != nil {
		t.Fatalf("seat 0 action failed: %v", err)
	}
}

func TestApplyCustomRMN(t *testing.T) {
	s := newStore(t)
	rm, _ := s.Create("")
	ids, _ := takeN(t, s, rm.ID, 2)
	if _, _, err := s.PickFaction(rm.ID, ids[0], "MC"); err != nil {
		t.Fatal(err)
	}
	_, g, err := s.PickFaction(rm.ID, ids[1], "ED")
	if err != nil {
		t.Fatal(err)
	}
	legal := g.LegalActions()
	if len(legal) == 0 {
		t.Fatal("expected a legal setup action")
	}
	// Apply the first legal action to a clone to learn the line it emits.
	c := g.Clone()
	if err := c.Apply(legal[0]); err != nil {
		t.Fatal(err)
	}
	line := c.RMNLog[len(c.RMNLog)-1]

	// The wrong seat cannot apply the line.
	if _, _, err := s.ApplyRMN(rm.ID, ids[1], line); err == nil {
		t.Fatal("seat 1 should not act on MC's setup")
	}
	// A malformed line is rejected with the engine's message.
	if _, _, err := s.ApplyRMN(rm.ID, ids[0], "not rmn at all"); err == nil {
		t.Fatal("a malformed line should be rejected")
	}
	// The right seat can apply the exact line.
	if _, _, err := s.ApplyRMN(rm.ID, ids[0], line); err != nil {
		t.Fatalf("custom RMN apply failed: %v", err)
	}
}

func TestRedaction(t *testing.T) {
	s := newStore(t)
	rm, _ := s.Create("")
	ids, _ := takeN(t, s, rm.ID, 4)
	for i, f := range []string{"MC", "ED", "WA", "VB"} {
		if _, _, err := s.PickFaction(rm.ID, ids[i], f); err != nil {
			t.Fatal(err)
		}
	}
	_, g, _ := s.Load(rm.ID)
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
	rm, _ := s.Create("")
	ids, _ := takeN(t, s, rm.ID, 2)
	if _, _, err := s.PickFaction(rm.ID, ids[0], "MC"); err != nil {
		t.Fatal(err)
	}
	started, g, err := s.PickFaction(rm.ID, ids[1], "ED")
	if err != nil {
		t.Fatal(err)
	}
	if !started.Started {
		t.Fatal("the game should have started")
	}
	// Drive setup through the store; this used to panic when the Eyrie chose a
	// leader because the setup machine assumed the Alliance was present.
	for g.SetupMode {
		acts := g.LegalActions()
		if len(acts) == 0 {
			t.Fatalf("stuck at stage %s", g.SetupStage)
		}
		seatID := ""
		for i, st := range started.Seats {
			if st.Occupied && st.Faction == string(g.Current) {
				seatID = ids[i]
			}
		}
		if _, _, err := s.ApplyAction(rm.ID, seatID, acts[0].ID); err != nil {
			t.Fatalf("apply %s: %v", acts[0].ID, err)
		}
		_, g, _ = s.Load(rm.ID)
	}
}

func TestListRooms(t *testing.T) {
	s := newStore(t)
	a, _ := s.Create("Alpha")
	b, _ := s.Create("Beta")
	list, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("list = %+v", list)
	}
	seen := map[string]bool{}
	for _, rm := range list {
		seen[rm.ID] = true
	}
	if !seen[a.ID] || !seen[b.ID] {
		t.Fatalf("missing rooms: %+v", list)
	}
}

func TestBotPlaysItsTurn(t *testing.T) {
	s := newStore(t)
	rm, _ := s.Create("")
	if _, _, err := s.AddBot(rm.ID, rm.Seats[0].ID, "MC", "greedy:full"); err != nil {
		t.Fatalf("add bot: %v", err)
	}
	if _, _, _, _, err := s.Take(rm.ID, rm.Seats[1].ID); err != nil {
		t.Fatalf("take: %v", err)
	}
	started, g, err := s.PickFaction(rm.ID, rm.Seats[1].ID, "ED")
	if err != nil {
		t.Fatalf("pick: %v", err)
	}
	if !started.Started {
		t.Fatal("the game should start with a bot and a human seated")
	}
	if len(started.Seats) != 4 {
		t.Fatalf("seats = %d, want 4", len(started.Seats))
	}
	// MC is first and is a bot, so it should have played its setup and stopped
	// at the human's ED step.
	if g.SetupMode && g.Actor() != root.ED {
		t.Fatalf("expected ED to act after the bot's setup, got %s (stage %s)", g.Actor(), g.SetupStage)
	}
}

func TestPendingPlayerActsDuringBattle(t *testing.T) {
	s, rm, g, seatOf := startedBattle(t)
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
	if _, _, err := s.ApplyAction(rm.ID, seatOf["MC"], hit.ID); err == nil {
		t.Fatal("the turn player should not act on the defender's pending hits")
	}
	if _, _, err := s.ApplyAction(rm.ID, seatOf["ED"], hit.ID); err != nil {
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
func startedBattle(t *testing.T) (*Store, *Room, *root.Game, map[string]string) {
	t.Helper()
	s := newStore(t)
	rm, _ := s.Create("")
	ids, _ := takeN(t, s, rm.ID, 2)
	if _, _, err := s.PickFaction(rm.ID, ids[0], "MC"); err != nil {
		t.Fatal(err)
	}
	started, g, err := s.PickFaction(rm.ID, ids[1], "ED")
	if err != nil {
		t.Fatal(err)
	}
	seatOf := map[string]string{}
	for i, st := range started.Seats {
		if st.Faction != "" {
			seatOf[st.Faction] = ids[i]
		}
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
