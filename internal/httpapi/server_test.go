package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Thanhphan1147/root-multiplayer/internal/room"
)

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	store := &room.Store{Dir: t.TempDir(), Secret: []byte("test")}
	srv := &Server{Store: store}
	return httptest.NewServer(srv.Handler())
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

func TestEndToEnd(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	_, created := do(t, ts, "POST", "/api/rooms", "", map[string]any{"players": 4}, nil)
	tokensAny, _ := created["tokens"].([]any)
	if len(tokensAny) != 4 {
		t.Fatalf("expected 4 tokens, got %v", created["tokens"])
	}
	tokens := make([]string, len(tokensAny))
	for i, v := range tokensAny {
		tokens[i] = v.(string)
	}

	for i, f := range []string{"MC", "ED", "WA", "VB"} {
		resp, out := do(t, ts, "POST", "/api/faction", tokens[i], map[string]any{"faction": f}, nil)
		if resp.StatusCode != 200 {
			t.Fatalf("pick %s: %d", f, resp.StatusCode)
		}
		if i == 3 {
			rm := out["room"].(map[string]any)
			if rm["started"] != true {
				t.Fatal("game should have started")
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
