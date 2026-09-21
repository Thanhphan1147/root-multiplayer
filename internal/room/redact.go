package room

import (
	"regexp"
	"strings"

	"github.com/Thanhphan1147/root-mn/pkg/root"
)

var reCardID = regexp.MustCompile(`[FRMB][0-9]{2}`)

// hiddenLogKinds are log entries whose text can reveal hidden cards.
var hiddenLogKinds = map[string]bool{
	"deal": true, "mobilize": true, "aid": true,
	"stand-deliver": true, "special": true, "outrage": true,
}

// hiddenRMNIntents are RMN intents whose operands can reveal hidden cards.
var hiddenRMNIntents = map[string]bool{
	"draw": true, "deal": true, "A:mobilize": true,
	"V:aid": true, "V:day-labor": true, "stand-deliver": true,
}

// Redact returns the per-viewer snapshot: other players' hands and supporters
// are hidden, the deck is removed, and hidden outcomes are masked.
func Redact(g *root.Game, viewer string) map[string]any {
	cp := g.Clone()
	for f, p := range cp.Players {
		if string(f) == viewer {
			continue
		}
		for i := range p.Hand {
			p.Hand[i] = "??"
		}
		for i := range p.Supporters {
			p.Supporters[i] = "??"
		}
	}
	cp.Deck = nil

	snap := root.Snapshot(cp)
	snap["hash"] = ""
	// The acting player is the pending player when the engine is waiting on a
	// deferred choice (battle hits, discards, field hospitals), otherwise the
	// turn player. Only they receive legal actions. Whose choice it is stays
	// visible to everyone so clients can show the turn indicator; any hidden
	// context is stripped.
	actor := g.Current
	if g.Pending != nil {
		actor = g.Pending.Player
	}
	if string(actor) != viewer {
		snap["legal"] = []root.Action{}
	}
	if g.Pending != nil {
		pend := *g.Pending
		pend.Context = nil
		snap["pending"] = &pend
	} else {
		snap["pending"] = nil
	}
	snap["log"] = redactLog(g.Log, viewer)
	snap["rmn"] = redactRMN(g.RMNLog, viewer)
	return snap
}

func redactLog(log []root.LogEntry, viewer string) []root.LogEntry {
	out := make([]root.LogEntry, len(log))
	copy(out, log)
	for i := range out {
		e := &out[i]
		if hiddenLogKinds[e.Kind] && string(e.Actor) != viewer {
			e.Text = redactCardIDs(e.Text)
			e.Data = nil
		}
	}
	return out
}

func redactRMN(lines []string, viewer string) []string {
	out := make([]string, len(lines))
	for i, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			out[i] = line
			continue
		}
		intent := fields[3]
		if hiddenRMNIntents[intent] && rmnOwner(fields) != viewer {
			out[i] = redactCardIDs(line)
		} else {
			out[i] = line
		}
	}
	return out
}

func rmnOwner(fields []string) string {
	for _, f := range fields[4:] {
		if strings.HasPrefix(f, "who=") {
			return strings.TrimPrefix(f, "who=")
		}
	}
	return fields[2]
}

func redactCardIDs(s string) string {
	return reCardID.ReplaceAllString(s, "??")
}
