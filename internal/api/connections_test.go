package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/redbotster/nanobots/internal/oneclaw"
)

// fakeOneClaw wires up just enough of the real 1Claw Human API — token
// exchange, vault ensure, and secret put/get, backed by an in-memory map —
// for the connections endpoints to exercise a real oneclaw.Client against,
// without a real account.
func fakeOneClaw(t *testing.T) *oneclaw.Client {
	c, _ := fakeOneClawCounting(t)
	return c
}

// fakeOneClawCounting is the same fake, plus a count of how many secret
// reads actually reached it — the only way to tell a served cache from a
// re-read, since both produce an identical response body.
func fakeOneClawCounting(t *testing.T) (*oneclaw.Client, *atomic.Int64) {
	t.Helper()
	var reads atomic.Int64
	var mu sync.Mutex
	secrets := map[string]string{}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/auth/api-key-token", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"access_token": "tok", "token_type": "Bearer", "expires_in": 86400})
	})
	mux.HandleFunc("/v1/vaults", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			json.NewEncoder(w).Encode(map[string]string{"id": "v1", "name": "nanobots-main"})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"vaults": []map[string]string{{"id": "v1", "name": "nanobots-main"}}})
	})
	mux.HandleFunc("/v1/vaults/v1/secrets/", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path[len("/v1/vaults/v1/secrets/"):]
		// The handler under test reads these concurrently now, so the map
		// behind them needs a lock or -race fails on the fake, not the code.
		mu.Lock()
		defer mu.Unlock()
		switch r.Method {
		case http.MethodPut:
			var body map[string]string
			json.NewDecoder(r.Body).Decode(&body)
			secrets[path] = body["value"]
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]any{"id": "s1", "path": path, "type": "generic", "version": 1})
		case http.MethodGet:
			reads.Add(1)
			val, ok := secrets[path]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"id": "s1", "path": path, "type": "generic", "value": val, "version": 1})
		}
	})
	mux.HandleFunc("/v1/agents", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			json.NewEncoder(w).Encode(map[string]any{
				"agent":   map[string]any{"id": "agent-1", "name": body["name"]},
				"api_key": "ocv_test",
			})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"agents": []map[string]string{}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	oc := oneclaw.NewClient("1ck_test")
	oc.BaseURL = srv.URL
	return oc, &reads
}

func testServerWithOneClaw(t *testing.T) *Server {
	t.Helper()
	srv := testServer(t)
	srv.OneClaw = fakeOneClaw(t)
	return srv
}

func TestHandleConnectionsStatusEmptyWhenOneClawNotConfigured(t *testing.T) {
	srv := testServer(t) // no OneClaw set
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/connections", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
	}
	var statuses []connectionStatus
	json.Unmarshal(rec.Body.Bytes(), &statuses)
	if len(statuses) != 0 {
		t.Errorf("statuses = %+v, want empty", statuses)
	}
}

func TestHandleConnectionsStatusReflectsWhatsInTheVault(t *testing.T) {
	srv := testServerWithOneClaw(t)

	// Before connecting anything, everything's false.
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/connections", nil))
	var before []connectionStatus
	json.Unmarshal(rec.Body.Bytes(), &before)
	for _, s := range before {
		if s.Connected {
			t.Errorf("expected %s to start disconnected, got %+v", s.Service, s)
		}
	}

	// Connect Slack via a pasted token.
	body, _ := json.Marshal(connectTokenRequest{Token: "xoxb-abc"})
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/connections/slack", bytes.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("connect slack: status = %d, body: %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/connections", nil))
	var after []connectionStatus
	json.Unmarshal(rec.Body.Bytes(), &after)
	found := false
	for _, s := range after {
		if s.Service == "slack" {
			found = true
			if !s.Connected {
				t.Error("expected slack to be connected after posting a token")
			}
		}
		if s.Service == "github" && s.Connected {
			t.Error("expected github to remain disconnected")
		}
	}
	if !found {
		t.Fatal("expected a slack entry in the status list")
	}
}

func TestHandleConnectTokenRejectsEmptyToken(t *testing.T) {
	srv := testServerWithOneClaw(t)
	body, _ := json.Marshal(connectTokenRequest{Token: "  "})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/connections/github", bytes.NewReader(body)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleConnectTokenRejectsUnknownService(t *testing.T) {
	srv := testServerWithOneClaw(t)
	body, _ := json.Marshal(connectTokenRequest{Token: "x"})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/connections/dropbox", bytes.NewReader(body)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleConnectTokenRejectsGoogleAsAPastedToken(t *testing.T) {
	// Google is a refresh-token/OAuth flow, not a static paste-a-token one —
	// must go through /api/connections/google/start instead.
	srv := testServerWithOneClaw(t)
	body, _ := json.Marshal(connectTokenRequest{Token: "x"})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/connections/google", bytes.NewReader(body)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleConnectGoogleStartRejectsWhenNoClientIDConfigured(t *testing.T) {
	t.Setenv("NANOBOTS_ENV_FILE", "/nonexistent/path/for/this/test.env")
	srv := testServerWithOneClaw(t)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/connections/google/start", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleConnectTokenRejectsXAndLinkedInAsPastedTokens(t *testing.T) {
	// X and LinkedIn are OAuth flows, like Google — must go through their
	// own /start endpoints instead of the generic paste-a-token path.
	srv := testServerWithOneClaw(t)
	for _, service := range []string{"x", "linkedin"} {
		body, _ := json.Marshal(connectTokenRequest{Token: "x"})
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/connections/"+service, bytes.NewReader(body)))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400, body: %s", service, rec.Code, rec.Body.String())
		}
	}
}

func TestHandleConnectXStartRejectsWhenNoClientIDConfigured(t *testing.T) {
	t.Setenv("NANOBOTS_ENV_FILE", "/nonexistent/path/for/this/test.env")
	srv := testServerWithOneClaw(t)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/connections/x/start", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleConnectLinkedInStartRejectsWhenNoClientCredentialsConfigured(t *testing.T) {
	t.Setenv("NANOBOTS_ENV_FILE", "/nonexistent/path/for/this/test.env")
	srv := testServerWithOneClaw(t)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/connections/linkedin/start", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleConnectionsStatusTreatsALinkedInAccessTokenAsConnected(t *testing.T) {
	// Exercises the fallback in handleConnectionsStatus: a LinkedIn account
	// connected without a refresh token (linkedin/access_token only) must
	// still report Connected: true.
	srv := testServerWithOneClaw(t)
	vault, err := srv.OneClaw.EnsureVault("nanobots-main")
	if err != nil {
		t.Fatalf("EnsureVault: %v", err)
	}
	if err := srv.OneClaw.PutSecret(vault.ID, "linkedin/access_token", "tok"); err != nil {
		t.Fatalf("PutSecret: %v", err)
	}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/connections", nil))
	var statuses []connectionStatus
	json.Unmarshal(rec.Body.Bytes(), &statuses)
	for _, s := range statuses {
		if s.Service == "linkedin" && !s.Connected {
			t.Error("expected linkedin to be Connected via its access_token fallback")
		}
	}
}

// The eight vault reads behind this endpoint run concurrently (7.8s serial,
// ~1s parallel against the live daemon). Two properties that a concurrent
// rewrite can quietly break, and neither shows up as a failing build:
//
//   - the order of the list, which has to stay fixed because the response
//     is ETag-cached and a reshuffle would invalidate it on every call
//   - the absence of a data race on the shared result slice
//
// The race is only visible under -race, which the house verification
// command already passes, so this test exists to give it something to look
// at: before this, nothing exercised the endpoint's goroutines at all.
func TestConnectionsStatusIsStablyOrderedUnderConcurrency(t *testing.T) {
	srv := testServerWithOneClaw(t)

	var first []connectionStatus
	for i := 0; i < 5; i++ {
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/connections", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
		}
		var got []connectionStatus
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if len(got) != len(connectionServices) {
			t.Fatalf("got %d statuses, want %d", len(got), len(connectionServices))
		}
		for j, svc := range connectionServices {
			if got[j].Service != svc {
				t.Fatalf("position %d = %q, want %q — the order must not depend on "+
					"which vault read finished first", j, got[j].Service, svc)
			}
		}
		if first == nil {
			first = got
			continue
		}
		for j := range got {
			if got[j] != first[j] {
				t.Fatalf("call %d differs at %d: %+v vs %+v", i, j, got[j], first[j])
			}
		}
	}
}

// Four pages load /api/connections, and answering it honestly costs eight
// vault round trips that 1Claw throttles — 7.8s serial, 3.2s concurrent,
// measured against the live account. So the answer is cached, and these are
// the two things that has to get right.
func TestConnectionsStatusIsCachedButNeverStaleAfterConnecting(t *testing.T) {
	srv := testServer(t)
	oc, reads := fakeOneClawCounting(t)
	srv.OneClaw = oc

	get := func() []connectionStatus {
		t.Helper()
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/connections", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
		}
		var out []connectionStatus
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	connected := func(list []connectionStatus, service string) bool {
		t.Helper()
		for _, s := range list {
			if s.Service == service {
				return s.Connected
			}
		}
		t.Fatalf("no %s in %+v", service, list)
		return false
	}

	get()
	afterFirst := reads.Load()
	if afterFirst == 0 {
		t.Fatal("the first call read nothing from the vault")
	}

	// Cached: three more calls, no further vault traffic.
	get()
	get()
	get()
	if got := reads.Load(); got != afterFirst {
		t.Errorf("vault reads went %d -> %d across three repeat calls; the cache is not being used", afterFirst, got)
	}

	// Never stale: connecting has to show immediately, not after the TTL.
	// This is the honesty half — a cache that keeps saying "not connected"
	// about something the user just connected is worse than a slow page.
	if connected(get(), "slack") {
		t.Fatal("slack started connected")
	}
	body, _ := json.Marshal(connectTokenRequest{Token: "xoxb-abc"})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/connections/slack", bytes.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("connect slack: %d %s", rec.Code, rec.Body.String())
	}
	if !connected(get(), "slack") {
		t.Error("slack still reads as disconnected right after connecting it — the cache outlived the write")
	}
}
