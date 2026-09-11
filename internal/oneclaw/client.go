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

// ensureToken exchanges the API key for a bearer token via
// POST /v1/auth/api-key-token, refreshing it a minute before expiry.
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
	req, err := http.NewRequest(http.MethodPost, c.BaseURL+"/v1/auth/api-key-token", bytes.NewReader(body))
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
