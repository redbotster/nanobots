package api

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/redbotster/nanobots/internal/oneclaw"
)

// The Shroud shim: an OpenAI-shaped endpoint that adds the two things
// Shroud needs and no chat-completions client sends.
//
// Shroud already speaks /v1/chat/completions, and it forwards tools
// faithfully — verified against the live service, including `tool_choice:
// "auto"` and OpenAI-shaped tool_calls coming back. What it also requires
// is an X-Shroud-Provider header and an agent id:key pair, neither of which
// a generic client can be told to send. Honcho, for instance, lets you
// override a base URL and an API-key env var, and nothing else.
//
// So this sits in front: take a bearer token, add the headers, forward the
// body untouched.
//
// The point is not convenience. The most sensitive text in this whole
// system is what goes into memory — what is in someone's inbox, what they
// promised whom, what they escalate. Right now that goes straight from a
// local Honcho to a model provider with nothing in between. Through here it
// gets the same treatment a nanobot's own prompts get: billed against a
// budget, PII and secrets redacted, screened for injection.
//
// Two honest limits, both in docs/llm.md:
//   - Embeddings still go direct. Shroud's embeddings route wants a
//     provider key stored in the 1Claw vault, and an agent is scoped to one
//     provider anyway, so the embedding model still sees observation text.
//   - The agent's AllowedProviders decides what may be asked for. Point a
//     client here with a model this agent can't call and Shroud answers 403,
//     which is the guardrail working.

// shroudProxyAgentName is the agent this shim bills against — its own,
// separate from any bot's, so the spend shows up as what it is and can
// carry its own budget.
const shroudProxyAgentName = "nanobots-shroud-proxy"

// shroudProxyDailyBudgetUSD caps what anything behind this shim can spend
// in a day. Deliberately modest: this exists to meter a background memory
// service, and a runaway deriver loop should hit a ceiling rather than a
// credit card.
const shroudProxyDailyBudgetUSD = 5

// ShroudProxy holds the shim's state: the shared token callers must present
// and the lazily-provisioned agent it bills against.
type ShroudProxy struct {
	// Token is what a caller must send as `Authorization: Bearer`. Written
	// to StateDir on first start so a restart doesn't invalidate a
	// running Honcho's config.
	Token string
	// Provider is what to tell Shroud when the caller doesn't say. Must be
	// one the agent is allowed to use.
	Provider string

	mu       sync.Mutex
	agentID  string
	agentKey string
}

// LoadShroudProxyToken returns the shim's shared token, creating it on
// first use.
//
// A token rather than an open endpoint because this one spends money.
// Everything else nanobotd serves on loopback is inert if someone reaches
// it; this is a metered LLM. And it is reachable from more places than the
// rest of the API — Docker containers get to the host through
// host.docker.internal even though nanobotd binds 127.0.0.1, which is
// exactly how Honcho reaches it.
func LoadShroudProxyToken(stateDir string) (string, error) {
	return loadOrCreateToken(filepath.Join(stateDir, "shroud-proxy-token"))
}

// loadOrCreateToken returns a stable per-install secret, minting one on
// first use. Shared by the Shroud proxy and the webhook trigger: both guard
// an endpoint that costs money or sends mail, and both must survive a
// restart or every caller configured against them breaks.
func loadOrCreateToken(path string) (string, error) {
	if raw, err := os.ReadFile(path); err == nil {
		if tok := strings.TrimSpace(string(raw)); tok != "" {
			return tok, nil
		}
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	tok := hex.EncodeToString(buf)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(tok+"\n"), 0o600); err != nil {
		return "", err
	}
	return tok, nil
}

// credentials provisions (once) the agent this shim bills against.
func (p *ShroudProxy) credentials(oc *oneclaw.Client, stateDir string) (id, key string, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.agentID != "" {
		return p.agentID, p.agentKey, nil
	}
	provider := p.Provider
	if provider == "" {
		provider = "anthropic"
	}
	id, key, err = oc.EnsureAgent(stateDir, shroudProxyAgentName, oneclaw.CreateAgentRequest{
		ShroudEnabled: true,
		ShroudConfig: &oneclaw.ShroudConfig{
			PIIPolicy:             "redact",
			EnableSecretRedaction: true,
			InjectionThreshold:    0.7,
			AllowedProviders:      []string{provider},
			DailyBudgetUSD:        shroudProxyDailyBudgetUSD,
		},
	})
	if err != nil {
		return "", "", err
	}
	p.agentID, p.agentKey = id, key
	return id, key, nil
}

// handleShroudProxy forwards one OpenAI-shaped request to Shroud.
//
// The body is passed through byte for byte. Rewriting it would mean this
// shim has an opinion about model names, tool schemas and sampling
// parameters, all of which change faster than this code would — and the
// whole value of speaking a de facto format is that both ends already agree
// on it.
func (s *Server) handleShroudProxy(w http.ResponseWriter, r *http.Request) {
	if s.Shroud == nil {
		writeError(w, http.StatusNotFound, fmt.Errorf("the Shroud proxy is not enabled on this daemon"))
		return
	}
	if !s.Shroud.authorised(r) {
		// No detail: an unauthenticated caller learns only that it was
		// refused, not whether the token was close.
		w.Header().Set("WWW-Authenticate", `Bearer realm="nanobots-shroud-proxy"`)
		writeError(w, http.StatusUnauthorized, fmt.Errorf("this endpoint needs the shroud proxy token"))
		return
	}
	if s.OneClaw == nil || !s.OneClaw.Configured() {
		writeError(w, http.StatusServiceUnavailable, fmt.Errorf("the Shroud proxy needs ONECLAW_API_KEY"))
		return
	}

	stateDir := ""
	if s.Orchestrator != nil {
		stateDir = s.Orchestrator.AgentStateDir
	}
	agentID, agentKey, err := s.Shroud.credentials(s.OneClaw, stateDir)
	if err != nil {
		writeError(w, http.StatusBadGateway, fmt.Errorf("provision the shroud proxy agent: %w", err))
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 8<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	// Everything after /shroud is the Shroud path, so a client configured
	// with base_url=".../shroud/v1" reaches /v1/chat/completions unchanged.
	upstreamPath := strings.TrimPrefix(r.URL.Path, "/shroud")
	if upstreamPath == "" || upstreamPath[0] != '/' {
		upstreamPath = "/" + upstreamPath
	}

	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost,
		strings.TrimRight(oneclaw.DefaultShroudURL, "/")+upstreamPath, strings.NewReader(string(body)))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Shroud-Agent-Key", agentID+":"+agentKey)
	provider := r.Header.Get("X-Shroud-Provider")
	if provider == "" {
		provider = s.Shroud.Provider
	}
	if provider == "" {
		provider = "anthropic"
	}
	req.Header.Set("X-Shroud-Provider", provider)

	resp, err := shroudProxyClient.Do(req)
	if err != nil {
		writeError(w, http.StatusBadGateway, fmt.Errorf("shroud: %w", err))
		return
	}
	defer resp.Body.Close()

	// Upstream's status and body verbatim: a 429 has to arrive as a 429 so
	// the caller's own backoff works, and Shroud's error messages are the
	// useful part of a failure.
	w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
	if ra := resp.Header.Get("Retry-After"); ra != "" {
		w.Header().Set("Retry-After", ra)
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

// authorised compares the bearer token in constant time, so a caller can't
// learn the token a byte at a time from response timing.
func (p *ShroudProxy) authorised(r *http.Request) bool {
	if p.Token == "" {
		return false
	}
	tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(strings.TrimSpace(tok)), []byte(p.Token)) == 1
}

// shroudProxyClient has no timeout of its own beyond a generous ceiling:
// the caller's context governs, and a dialectic query with several rounds
// of tool use legitimately takes a while.
var shroudProxyClient = &http.Client{Timeout: 10 * time.Minute}
