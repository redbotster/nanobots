// Package oneclaw is the real 1Claw Human API client: API-key auth, vaults,
// agents, OAuth connect, execution bindings/intents. Request and response
// shapes here were verified against the live API (curl, with the real key
// redacted) and against @1claw/openapi-spec, not guessed from prose docs —
// see docs/oneclaw-bridge.md for the endpoints and why each shape is what it
// is.
//
// The API key never touches disk or a log line anywhere in this package: it
// lives in Client.apiKey for the process lifetime and is exchanged for a
// short-lived bearer token on first use.
package oneclaw

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// DefaultBaseURL is a var, not a const, purely so tests can point a
// throwaway Client at an httptest server instead of the real 1Claw API —
// mirrors DefaultShroudURL in shroud.go for the same reason.
var DefaultBaseURL = "https://api.1claw.co"

// DefaultEnvFilePath resolves the dotenv file every LoadEnvValue caller
// reads from by default: $NANOBOTS_ENV_FILE, falling back to
// ~/.secrets/nanobots.env.
func DefaultEnvFilePath() (string, error) {
	if p := os.Getenv("NANOBOTS_ENV_FILE"); p != "" {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return home + "/.secrets/nanobots.env", nil
}

// LoadEnvValue reads one KEY=VALUE line out of a dotenv-style file ('#'
// comments allowed, quotes around the value stripped). path="" uses
// DefaultEnvFilePath(). Returns ("", nil) if the file or the key is absent —
// callers decide what an absent value means for them.
func LoadEnvValue(path, key string) (string, error) {
	if path == "" {
		var err error
		path, err = DefaultEnvFilePath()
		if err != nil {
			return "", err
		}
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(k) != key {
			continue
		}
		v = strings.TrimSpace(v)
		v = strings.Trim(v, `"'`)
		return v, nil
	}
	return "", sc.Err()
}

// WriteEnvValue upserts one KEY=VALUE line in the same dotenv-style file
// LoadEnvValue reads — every other line (including comments and every
// other key) is left exactly as it was. path="" uses DefaultEnvFilePath().
// Creates the file (and its directory) at mode 0600 if it doesn't exist
// yet, since this file holds real credentials — matching the sensitivity
// internal/oneclaw/state.go already treats agent credentials with.
//
// This exists so the WebUI's Settings page can offer "paste your key here"
// for ONECLAW_API_KEY the same way it already does for a Slack/GitHub
// token — those land in a 1Claw vault; this one can't (nothing can decrypt
// a vault without it), so the dotenv file is genuinely the only place for
// it to live, but a human should never have to open a text editor to put
// it there.
func WriteEnvValue(path, key, value string) error {
	if path == "" {
		var err error
		path, err = DefaultEnvFilePath()
		if err != nil {
			return err
		}
	}

	var lines []string
	if raw, err := os.ReadFile(path); err == nil {
		lines = strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
		if len(lines) == 1 && lines[0] == "" {
			lines = nil
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	newLine := key + "=" + value
	replaced := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		k, _, ok := strings.Cut(trimmed, "=")
		if ok && strings.TrimSpace(k) == key {
			lines[i] = newLine
			replaced = true
			break
		}
	}
	if !replaced {
		lines = append(lines, newLine)
	}

	if dir := dirOf(path); dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600)
}

func dirOf(path string) string {
	i := strings.LastIndexByte(path, '/')
	if i < 0 {
		return ""
	}
	return path[:i]
}

// LoadAPIKey reads ONECLAW_API_KEY from a dotenv-style file. path defaults
// to $NANOBOTS_ENV_FILE, then ~/.secrets/nanobots.env. Returns ("", nil) if
// the file or the key is absent — the caller decides whether that means
// "run in demo mode".
func LoadAPIKey(path string) (string, error) {
	return LoadEnvValue(path, "ONECLAW_API_KEY")
}

// Client is a 1Claw Human API client bound to one API key.
type Client struct {
	BaseURL    string
	HTTPClient *http.Client

	apiKey string

	// tokenPath is which exchange endpoint this client uses. Empty means
	// the human one; NewAgentClient sets the agent one.
	tokenPath string

	mu          sync.Mutex
	token       string
	tokenExpiry time.Time

	// Short-lived cache of the agent listing, so EnsureAgent can verify a
	// saved credential still points at a live agent without one API call
	// per bot in a swarm. See agents.go's listAgentsCached.
	agentsMu sync.Mutex
	agents   []Agent
	agentsAt time.Time
}

func NewClient(apiKey string) *Client {
	return &Client{
		BaseURL:    DefaultBaseURL,
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
		apiKey:     apiKey,
	}
}

// Configured reports whether this client has an API key at all — callers use
// this to decide whether to fall back to demo mode.
func (c *Client) Configured() bool { return c.apiKey != "" }

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
}

// agentAuth switches the token exchange to the agent endpoint.
//
// Both endpoints take the same {"api_key": ...} body and return the same
// {"access_token": ...}; only the path and the privilege differ. A Human
// key (1ck_) goes to /v1/auth/api-key-token and can do everything. An agent
// key (ocv_) goes to /v1/auth/agent-token and is deliberately narrower: it
// reads and writes its own vault, requests approvals, and reads
// automations, but the control plane refuses it — /v1/otel/* answers 403
// "Control-plane token required", and installing a connector, creating a
// binding or deciding an approval are all human-only.
//
// Worth having as one flag rather than a second client type: everything
// else about talking to 1Claw is identical, and two near-copies of this
// file would drift.
const (
	humanTokenPath = "/v1/auth/api-key-token"
	agentTokenPath = "/v1/auth/agent-token"
)

// NewAgentClient authenticates as a 1Claw agent rather than as the human who
// owns the account. Use it for anything an agent is allowed to do on its own
// behalf — requesting an approval is the one that matters, since
// /v1/approvals/request refuses a Human key with "Only agents can request
// approvals".
func NewAgentClient(agentKey string) *Client {
	c := NewClient(agentKey)
	c.tokenPath = agentTokenPath
	return c
}

// ensureToken exchanges the API key for a bearer token, refreshing it a
// minute before expiry.
func (c *Client) ensureToken() (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" && time.Now().Before(c.tokenExpiry.Add(-1*time.Minute)) {
		return c.token, nil
	}
	if c.apiKey == "" {
		return "", fmt.Errorf("oneclaw: no API key configured")
	}
	body, _ := json.Marshal(map[string]string{"api_key": c.apiKey})
	path := c.tokenPath
	if path == "" {
		path = humanTokenPath
	}
	req, err := http.NewRequest(http.MethodPost, c.BaseURL+path, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("oneclaw: auth request failed: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("oneclaw: auth failed (%d): %s", resp.StatusCode, truncate(raw))
	}
	var tr tokenResponse
	if err := json.Unmarshal(raw, &tr); err != nil {
		return "", fmt.Errorf("oneclaw: parse auth response: %w", err)
	}
	c.token = tr.AccessToken
	c.tokenExpiry = time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second)
	return c.token, nil
}

// apiError is returned for any non-2xx response, carrying the status code
// and body so callers can distinguish e.g. 202 approval_required from a hard
// failure without string-matching.
type apiError struct {
	Status int
	Body   []byte
}

func (e *apiError) Error() string {
	return fmt.Sprintf("oneclaw: request failed (%d): %s", e.Status, truncate(e.Body))
}

// rawRequest performs an authenticated JSON request and returns the status
// code and body verbatim — the HTTP mechanics only, no opinion about which
// status codes mean success. 1Claw uses 202 for two different things
// depending on the endpoint (an accepted-and-pending approval request vs.
// /execute's "this needs a human decision first"), so that judgment belongs
// to each method, not to one shared status check.
func (c *Client) rawRequest(method, path string, in any) (status int, body []byte, err error) {
	token, err := c.ensureToken()
	if err != nil {
		return 0, nil, err
	}
	var bodyReader io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return 0, nil, err
		}
		bodyReader = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.BaseURL+path, bodyReader)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("oneclaw: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, raw, nil
}

// do performs an authenticated JSON request, treating any 2xx as success.
// out may be nil for responses the caller doesn't need.
func (c *Client) do(method, path string, in, out any) error {
	status, raw, err := c.rawRequest(method, path, in)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return &apiError{Status: status, Body: raw}
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("oneclaw: parse response from %s %s: %w", method, path, err)
		}
	}
	return nil
}

func truncate(b []byte) string {
	const max = 500
	s := string(b)
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}

// VaultLockedError means the vault exists and the credential is very likely
// in it — 1Claw just won't hand it over until the human re-verifies their
// passkey. It recurs in normal use (the lock is time-based), and it is a
// completely different problem from "this account was never connected", with
// a completely different fix, so callers must be able to tell them apart.
// Before this they couldn't, and a locked vault was reported to users as "no
// connected account yet — connect it from Settings", which is both wrong and
// useless advice.
type VaultLockedError struct{ Detail string }

func (e *VaultLockedError) Error() string {
	if e.Detail != "" {
		return "1Claw vault is locked: " + e.Detail
	}
	return "1Claw vault is locked — unlock it with your passkey in the 1Claw app"
}

// AsVaultLocked reports whether err is 1Claw refusing vault access pending
// passkey verification, returning a typed error with 1Claw's own wording.
//
// The signal is a 403 whose body mentions passkey verification. Matching on
// the body is not ideal, but 1Claw returns `"type":"about:blank"` for this,
// so there is no machine-readable code to key off — and silently treating it
// as a generic 403 is worse. If 1Claw ever gives it a real type, switch to
// that and delete the string match.
func AsVaultLocked(err error) (*VaultLockedError, bool) {
	var locked *VaultLockedError
	if errors.As(err, &locked) {
		return locked, true
	}
	var apiErr *apiError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusForbidden {
		return nil, false
	}
	body := string(apiErr.Body)
	if !strings.Contains(strings.ToLower(body), "passkey") {
		return nil, false
	}
	return &VaultLockedError{Detail: detailFromProblemJSON(apiErr.Body)}, true
}

// detailFromProblemJSON pulls the human-readable "detail" out of 1Claw's
// RFC 7807-shaped error body, falling back to the raw body if it isn't that
// shape — the point is to show the user 1Claw's own sentence, not our
// paraphrase of it.
func detailFromProblemJSON(body []byte) string {
	var problem struct {
		Detail string `json:"detail"`
	}
	if err := json.Unmarshal(body, &problem); err == nil && problem.Detail != "" {
		return problem.Detail
	}
	return strings.TrimSpace(truncate(body))
}

// AgentLimitError means the account is at its plan's agent cap. Like a
// locked vault, this arrives as a 403 that callers used to pass through as a
// raw JSON blob mid-run — see AsVaultLocked for the same reasoning.
type AgentLimitError struct{ Detail string }

func (e *AgentLimitError) Error() string {
	if e.Detail != "" {
		return "1Claw agent limit reached: " + e.Detail
	}
	return "1Claw agent limit reached"
}

// AsAgentLimit reports whether err is 1Claw refusing to create an agent
// because the account is at its cap.
//
// 1Claw does give this one a machine-readable type ("resource_limit_exceeded"),
// unlike the passkey 403, so this matches on that rather than on prose.
func AsAgentLimit(err error) (*AgentLimitError, bool) {
	var limit *AgentLimitError
	if errors.As(err, &limit) {
		return limit, true
	}
	var apiErr *apiError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusForbidden {
		return nil, false
	}
	var problem struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(apiErr.Body, &problem); err != nil || problem.Type != "resource_limit_exceeded" {
		return nil, false
	}
	return &AgentLimitError{Detail: detailFromProblemJSON(apiErr.Body)}, true
}
