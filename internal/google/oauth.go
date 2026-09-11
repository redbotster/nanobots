// Package google implements real Gmail/Drive access via a standard PKCE
// OAuth flow against Google directly — the documented fallback for
// bots/*/nanobot.yaml's Google services, which run on `connection: demo`
// fixtures until this is wired up with a real client id (see
// docs/connections.md for why: 1Claw's own Google OAuth provider has no
// Gmail scopes, and Browser Bridge is blocked outright by Google's
// anti-automation policy on the sign-in page itself).
//
// This needs a Google OAuth "Desktop app" client id — no client secret, a
// PKCE-only installed-app flow — provided via GOOGLE_OAUTH_CLIENT_ID in the
// same env file as ONECLAW_API_KEY (see internal/oneclaw.LoadAPIKey). It's
// deliberately per-user (one Google account, refresh token stored once in
// 1Claw), not per-bot — every bot that needs Gmail/Drive shares it.
package google

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const authEndpoint = "https://accounts.google.com/o/oauth2/v2/auth"

// tokenEndpoint is a var, not a const, purely so tests can point it at an
// httptest server instead of the real Google endpoint.
var tokenEndpoint = "https://oauth2.googleapis.com/token"

const (
	// Scopes every launch bot in this build needs, in one consent screen —
	// simpler for a non-technical user than connecting each bot separately,
	// and Google's consent screen shows exactly what's being granted either
	// way.
	ScopeGmailReadonly = "https://www.googleapis.com/auth/gmail.readonly"
	ScopeGmailSend     = "https://www.googleapis.com/auth/gmail.send"
	ScopeGmailCompose  = "https://www.googleapis.com/auth/gmail.compose"
	ScopeGmailModify   = "https://www.googleapis.com/auth/gmail.modify"
	ScopeDriveFile     = "https://www.googleapis.com/auth/drive.file"
	ScopeDriveReadonly = "https://www.googleapis.com/auth/drive.readonly"
)

// DefaultScopes covers every Google op this build's bots declare.
var DefaultScopes = []string{
	ScopeGmailReadonly, ScopeGmailSend, ScopeGmailCompose, ScopeGmailModify,
	ScopeDriveFile, ScopeDriveReadonly,
}

// PKCE is one authorization attempt's verifier/challenge/state triple.
// Generate a fresh one per Connect call — never reuse across attempts.
type PKCE struct {
	Verifier  string
	Challenge string
	State     string
}

func NewPKCE() (*PKCE, error) {
	verifier, err := randomURLSafeString(64)
	if err != nil {
		return nil, err
	}
	state, err := randomURLSafeString(32)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	return &PKCE{Verifier: verifier, Challenge: challenge, State: state}, nil
}

func randomURLSafeString(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// AuthURL builds the URL a human opens to grant access.
func AuthURL(clientID, redirectURI string, scopes []string, pkce *PKCE) string {
	q := url.Values{
		"client_id":             {clientID},
		"redirect_uri":          {redirectURI},
		"response_type":         {"code"},
		"scope":                 {strings.Join(scopes, " ")},
		"code_challenge":        {pkce.Challenge},
		"code_challenge_method": {"S256"},
		"state":                 {pkce.State},
		// offline + consent guarantees a refresh_token comes back even if
		// this same Google account granted these scopes before.
		"access_type": {"offline"},
		"prompt":      {"consent"},
	}
	return authEndpoint + "?" + q.Encode()
}

// TokenResponse mirrors Google's token endpoint response.
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"` // only present on the first exchange
	ExpiresIn    int    `json:"expires_in"`
	TokenType    string `json:"token_type"`
	Scope        string `json:"scope"`
}

// ExchangeCode trades an authorization code for tokens.
func ExchangeCode(clientID, code, verifier, redirectURI string) (*TokenResponse, error) {
	return doTokenRequest(url.Values{
		"client_id":     {clientID},
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"code_verifier": {verifier},
		"redirect_uri":  {redirectURI},
	})
}

// RefreshAccessToken mints a new access token from a stored refresh token.
func RefreshAccessToken(clientID, refreshToken string) (*TokenResponse, error) {
	return doTokenRequest(url.Values{
		"client_id":     {clientID},
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
	})
}

func doTokenRequest(form url.Values) (*TokenResponse, error) {
	req, err := http.NewRequest(http.MethodPost, tokenEndpoint, bytes.NewReader([]byte(form.Encode())))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("google: token request failed: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("google: token request rejected (%d): %s", resp.StatusCode, truncate(raw))
	}
	var tr TokenResponse
	if err := json.Unmarshal(raw, &tr); err != nil {
		return nil, fmt.Errorf("google: parse token response: %w", err)
	}
	return &tr, nil
}

func truncate(b []byte) string {
	const max = 400
	s := string(b)
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}
