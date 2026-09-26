package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Thanhphan1147/root-multiplayer/internal/room"
	"github.com/gorilla/websocket"
)

func newTestServer(t *testing.T) (*httptest.Server, *room.Store) {
	t.Helper()
	store := &room.Store{Dir: t.TempDir(), Secret: []byte("test")}
	srv := &Server{Store: store}
	return httptest.NewServer(srv.Handler()), store
}

func do(t *testing.T, ts *httptest.Server, method, path, token string, body any, headers map[string]string) (*http.Response, map[string]any) {
	t.Helper()
	var rdr *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	} else {
		rdr = bytes.NewReader(nil)
	}
	req, _ := http.NewRequest(method, ts.URL+path, rdr)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if resp.StatusCode != http.StatusNotModified {
		_ = json.NewDecoder(resp.Body).Decode(&out)
		resp.Body.Close()
	}
	return resp, out
}

// takeAll takes every seat over HTTP and returns the tokens.
func takeAll(t *testing.T, ts *httptest.Server, rm *room.Room) []string {
	t.Helper()
	tokens := make([]string, len(rm.Seats))
	for i, st := range rm.Seats {
		resp, out := do(t, ts, "POST", "/api/take", "", map[string]any{"room": rm.ID, "seat": st.ID}, nil)
		if resp.StatusCode != 200 {
			t.Fatalf("take seat %d: %d (%v)", i, resp.StatusCode, out)
		}
		tokens[i], _ = out["token"].(string)
	}
	return tokens
}

func TestEndToEnd(t *testing.T) {
	ts, store := newTestServer(t)
	defer ts.Close()

	rm, err := store.Create("Test")
	if err != nil {
		t.Fatal(err)
	}
	tokens := takeAll(t, ts, rm)

	for i, f := range []string{"MC", "ED", "WA", "VB"} {
		resp, out := do(t, ts, "POST", "/api/faction", tokens[i], map[string]any{"faction": f}, nil)
		if resp.StatusCode != 200 {
			t.Fatalf("pick %s: %d", f, resp.StatusCode)
		}
		if i == 3 {
			if out["room"].(map[string]any)["started"] != true {
				t.Fatal("the game should have started")
			}
		}
	}

	resp, state := do(t, ts, "GET", "/api/state", tokens[0], nil, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("state: %d", resp.StatusCode)
	}
	etag := resp.Header.Get("ETag")
	if etag == "" {
		t.Fatal("missing ETag")
	}
	if state["you"] != "MC" {
		t.Fatalf("you = %v", state["you"])
	}

	// Conditional fetch -> 304.
	resp2, _ := do(t, ts, "GET", "/api/state", tokens[0], nil, map[string]string{"If-None-Match": etag})
	if resp2.StatusCode != http.StatusNotModified {
		t.Fatalf("expected 304, got %d", resp2.StatusCode)
	}

	// The wrong seat cannot act; the right seat can.
	legal, _ := state["legal"].([]any)
	if len(legal) == 0 {
		t.Fatal("expected setup actions")
	}
	id := legal[0].(map[string]any)["id"].(string)
	if resp, _ := do(t, ts, "POST", "/api/action", tokens[1], map[string]any{"id": id}, nil); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("wrong seat action should be 403, got %d", resp.StatusCode)
	}
	if resp, _ := do(t, ts, "POST", "/api/action", tokens[0], map[string]any{"id": id}, nil); resp.StatusCode != 200 {
		t.Fatalf("right seat action should be 200, got %d", resp.StatusCode)
	}

	// Export the .rmn log.
	resp3, err := http.Get(ts.URL + "/api/export?token=" + tokens[0])
	if err != nil {
		t.Fatal(err)
	}
	defer resp3.Body.Close()
	if resp3.StatusCode != 200 {
		t.Fatalf("export: %d", resp3.StatusCode)
	}
	buf := new(bytes.Buffer)
	_, _ = buf.ReadFrom(resp3.Body)
	if !bytes.HasPrefix(buf.Bytes(), []byte("%RMN 3.0")) {
		t.Fatalf("export should start with %%RMN 3.0: %q", buf.String())
	}
}

func TestListAndTakeHTTP(t *testing.T) {
	ts, store := newTestServer(t)
	defer ts.Close()

	rm, err := store.Create("Alpha")
	if err != nil {
		t.Fatal(err)
	}

	resp, list := do(t, ts, "GET", "/api/rooms", "", nil, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("list: %d", resp.StatusCode)
	}
	if rooms, _ := list["rooms"].([]any); len(rooms) != 1 {
		t.Fatalf("expected 1 room, got %v", list["rooms"])
	}

	resp, out := do(t, ts, "POST", "/api/take", "", map[string]any{"room": rm.ID, "seat": rm.Seats[0].ID}, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("take: %d (%v)", resp.StatusCode, out)
	}
	tok, _ := out["token"].(string)
	if tok == "" {
		t.Fatal("take should return a token")
	}
	resp, state := do(t, ts, "GET", "/api/state", tok, nil, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("state with taken token: %d", resp.StatusCode)
	}
	if _, ok := state["room"]; !ok {
		t.Fatalf("state missing room: %v", state)
	}
}

func TestCustomRMNOverHTTP(t *testing.T) {
	ts, store := newTestServer(t)
	defer ts.Close()

	rm, _ := store.Create("")
	_, take0 := do(t, ts, "POST", "/api/take", "", map[string]any{"room": rm.ID, "seat": rm.Seats[0].ID}, nil)
	_, take1 := do(t, ts, "POST", "/api/take", "", map[string]any{"room": rm.ID, "seat": rm.Seats[1].ID}, nil)
	tok0, _ := take0["token"].(string)
	tok1, _ := take1["token"].(string)
	do(t, ts, "POST", "/api/faction", tok0, map[string]any{"faction": "MC"}, nil)
	do(t, ts, "POST", "/api/faction", tok1, map[string]any{"faction": "ED"}, nil)

	// Derive the first legal setup line by applying it to a clone.
	_, g, err := store.Load(rm.ID)
	if err != nil {
		t.Fatal(err)
	}
	legal := g.LegalActions()
	if len(legal) == 0 {
		t.Fatal("expected a legal setup action")
	}
	c := g.Clone()
	if err := c.Apply(legal[0]); err != nil {
		t.Fatal(err)
	}
	line := c.RMNLog[len(c.RMNLog)-1]

	// A malformed line is rejected.
	if resp, _ := do(t, ts, "POST", "/api/rmn", tok0, map[string]any{"line": "garbage"}, nil); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("malformed line should be 403, got %d", resp.StatusCode)
	}
	// The wrong seat cannot act on MC's setup.
	if resp, _ := do(t, ts, "POST", "/api/rmn", tok1, map[string]any{"line": line}, nil); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("wrong seat should be 403, got %d", resp.StatusCode)
	}
	// The right seat can apply the line.
	resp, out := do(t, ts, "POST", "/api/rmn", tok0, map[string]any{"line": line}, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("custom RMN should be 200, got %d (%v)", resp.StatusCode, out)
	}
}

func TestLeaveInvalidatesToken(t *testing.T) {
	ts, store := newTestServer(t)
	defer ts.Close()

	rm, _ := store.Create("")
	_, out := do(t, ts, "POST", "/api/take", "", map[string]any{"room": rm.ID, "seat": rm.Seats[0].ID}, nil)
	tok, _ := out["token"].(string)
	if tok == "" {
		t.Fatal("take should return a token")
	}
	if resp, _ := do(t, ts, "POST", "/api/leave", tok, nil, nil); resp.StatusCode != 200 {
		t.Fatalf("leave: %d", resp.StatusCode)
	}
	if resp, _ := do(t, ts, "GET", "/api/state", tok, nil, nil); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("state after leave should be 401, got %d", resp.StatusCode)
	}
}

func TestWebSocketNotify(t *testing.T) {
	ts, store := newTestServer(t)
	defer ts.Close()

	rm, _ := store.Create("")
	tokens := takeAll(t, ts, rm)
	for i, f := range []string{"MC", "ED", "WA", "VB"} {
		do(t, ts, "POST", "/api/faction", tokens[i], map[string]any{"faction": f}, nil)
	}

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/ws"
	c, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.WriteJSON(map[string]string{"token": tokens[0]}); err != nil {
		t.Fatal(err)
	}
	var ready map[string]any
	if err := c.ReadJSON(&ready); err != nil {
		t.Fatal(err)
	}
	if ready["type"] != "ready" {
		t.Fatalf("expected ready, got %v", ready)
	}

	// Trigger an action; the socket should receive a "changed" nudge.
	_, state := do(t, ts, "GET", "/api/state", tokens[0], nil, nil)
	legal := state["legal"].([]any)
	id := legal[0].(map[string]any)["id"].(string)
	do(t, ts, "POST", "/api/action", tokens[0], map[string]any{"id": id}, nil)

	_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
	var changed map[string]any
	if err := c.ReadJSON(&changed); err != nil {
		t.Fatalf("expected a change notification: %v", err)
	}
	if changed["type"] != "changed" {
		t.Fatalf("expected changed, got %v", changed)
	}
}
