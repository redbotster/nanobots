package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/redbotster/nanobots/internal/oneclaw"
)

// proxyServer wires the shim to a fake Shroud so a test can see exactly
// what reached upstream.
type upstreamCapture struct {
	path     string
	headers  http.Header
	body     string
	status   int
	response string
	after    string
}

func proxyServer(t *testing.T, up *upstreamCapture) *Server {
	t.Helper()
	srv := testServer(t)
	srv.OneClaw = fakeOneClaw(t)
	srv.Orchestrator.AgentStateDir = t.TempDir()
	srv.Shroud = &ShroudProxy{Token: "test-token", Provider: "anthropic"}

	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		up.path, up.headers, up.body = r.URL.Path, r.Header.Clone(), string(raw)
		if up.after != "" {
			w.Header().Set("Retry-After", up.after)
		}
		w.Header().Set("Content-Type", "application/json")
		if up.status != 0 {
			w.WriteHeader(up.status)
		}
		_, _ = w.Write([]byte(up.response))
	}))
	t.Cleanup(fake.Close)
	orig := oneclaw.DefaultShroudURL
	oneclaw.DefaultShroudURL = fake.URL
	t.Cleanup(func() { oneclaw.DefaultShroudURL = orig })
	return srv
}

func proxyPost(srv *Server, token, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader([]byte(body)))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

// The two things Shroud needs and no chat-completions client sends: the
// provider header and the agent id:key pair. Adding them is the shim's
// entire job.
func TestTheShimAddsWhatShroudNeedsAndChangesNothingElse(t *testing.T) {
	up := &upstreamCapture{response: `{"choices":[{"message":{"content":"ok"}}]}`}
	srv := proxyServer(t, up)

	// A body with tools in it — Shroud forwards these faithfully (verified
	// live), and the whole point of Honcho going through here is that its
	// dialectic tool calls survive.
	body := `{"model":"claude-sonnet-4-6","tools":[{"type":"function","function":{"name":"search_memory"}}],"tool_choice":"auto","messages":[{"role":"user","content":"hi"}]}`
	rec := proxyPost(srv, "test-token", "/shroud/v1/chat/completions", body)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
	}
	if up.path != "/v1/chat/completions" {
		t.Errorf("upstream path = %q — the /shroud prefix must be stripped, nothing else", up.path)
	}
	if up.headers.Get("X-Shroud-Provider") != "anthropic" {
		t.Errorf("X-Shroud-Provider = %q", up.headers.Get("X-Shroud-Provider"))
	}
	if !strings.Contains(up.headers.Get("X-Shroud-Agent-Key"), ":") {
		t.Errorf("X-Shroud-Agent-Key = %q, want id:key", up.headers.Get("X-Shroud-Agent-Key"))
	}
	// Byte for byte. Rewriting the body would mean this shim has an opinion
	// about model names, tool schemas and sampling parameters — all of
	// which change faster than this code would.
	if up.body != body {
		t.Errorf("the body was rewritten:\n got %s\nwant %s", up.body, body)
	}
	// And the caller's own bearer must not be forwarded as though it were a
	// provider credential.
	if got := up.headers.Get("Authorization"); got != "" {
		t.Errorf("forwarded the shim's own token upstream: %q", got)
	}
}

// This endpoint spends money, and unlike the rest of the API it is
// reachable from Docker containers through host.docker.internal even though
// nanobotd binds loopback. So it is the one route that authenticates.
func TestTheShimRefusesWithoutTheToken(t *testing.T) {
	up := &upstreamCapture{response: `{}`}
	srv := proxyServer(t, up)

	for _, tc := range []struct{ name, token string }{
		{"no token", ""},
		{"wrong token", "not-it"},
		{"prefix of the real token", "test-tok"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := proxyPost(srv, tc.token, "/shroud/v1/chat/completions", `{}`)
			if rec.Code != http.StatusUnauthorized {
				t.Errorf("status = %d, want 401", rec.Code)
			}
			if up.path != "" {
				t.Errorf("an unauthenticated request reached upstream at %q", up.path)
			}
		})
	}
}

// A 429 has to arrive at the caller as a 429, with Retry-After intact, or
// the caller's own backoff is flying blind — and Shroud's message is the
// useful half of any failure.
func TestUpstreamStatusAndRetryAfterArriveIntact(t *testing.T) {
	up := &upstreamCapture{
		status:   http.StatusTooManyRequests,
		after:    "17",
		response: `{"error":{"message":"daily budget exhausted"}}`,
	}
	srv := proxyServer(t, up)

	rec := proxyPost(srv, "test-token", "/shroud/v1/chat/completions", `{}`)
	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want the upstream 429", rec.Code)
	}
	if rec.Header().Get("Retry-After") != "17" {
		t.Errorf("Retry-After = %q", rec.Header().Get("Retry-After"))
	}
	if !strings.Contains(rec.Body.String(), "daily budget exhausted") {
		t.Errorf("upstream's own message was lost: %s", rec.Body.String())
	}
}

// A client that does know about providers can say so; one that doesn't gets
// the configured default. Honcho is the latter — it can be given a base URL
// and an API key, and nothing else.
func TestTheProviderCanBeOverriddenPerRequest(t *testing.T) {
	up := &upstreamCapture{response: `{}`}
	srv := proxyServer(t, up)

	req := httptest.NewRequest(http.MethodPost, "/shroud/v1/chat/completions", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("X-Shroud-Provider", "openai")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if up.headers.Get("X-Shroud-Provider") != "openai" {
		t.Errorf("X-Shroud-Provider = %q, want the caller's choice", up.headers.Get("X-Shroud-Provider"))
	}
}

// Not enabled means not there. A daemon without the shim configured must
// not expose a money-spending route at all.
func TestTheShimIsAbsentUnlessConfigured(t *testing.T) {
	srv := testServer(t) // no Shroud field
	rec := proxyPost(srv, "test-token", "/shroud/v1/chat/completions", `{}`)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

// The token has to survive a restart, or every nanobotd restart silently
// breaks a running Honcho's config.
func TestTheTokenIsStableAcrossRestarts(t *testing.T) {
	dir := t.TempDir()
	first, err := LoadShroudProxyToken(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) < 32 {
		t.Errorf("token is only %d chars — too short to be worth having", len(first))
	}
	second, err := LoadShroudProxyToken(dir)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Errorf("token changed across restarts: %q then %q", first, second)
	}

	// And it is not world-readable: it authorises spending.
	info, err := os.Stat(filepath.Join(dir, "shroud-proxy-token"))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("token file mode = %o, want 600", perm)
	}
}

// A separate agent, so the spend shows up as what it is and carries its own
// ceiling rather than quietly eating a bot's budget.
func TestTheShimBillsItsOwnAgentWithItsOwnBudget(t *testing.T) {
	up := &upstreamCapture{response: `{}`}
	srv := proxyServer(t, up)
	proxyPost(srv, "test-token", "/shroud/v1/chat/completions", `{}`)

	raw, err := os.ReadFile(filepath.Join(srv.Orchestrator.AgentStateDir, shroudProxyAgentName+".json"))
	if err != nil {
		t.Fatalf("no agent was provisioned for the shim: %v", err)
	}
	var creds map[string]string
	if err := json.Unmarshal(raw, &creds); err != nil {
		t.Fatal(err)
	}
	if creds["agent_id"] == "" {
		t.Error("agent has no id")
	}
}
