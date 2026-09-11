package oauth2pkce

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Config describes one provider's OAuth2 + PKCE endpoints and app
// registration. ClientSecret is empty for a true "public client" flow
// (Google, X — PKCE replaces the secret entirely); a few providers (LinkedIn)
// require a client secret on the token exchange even when PKCE is also used,
// so it's here, sent only when non-empty, rather than forcing every provider
// through a public-client shape that doesn't actually fit all of them.
type Config struct {
	AuthEndpoint    string
	TokenEndpoint   string
	ClientID        string
	ClientSecret    string
	Scopes          []string
	ExtraAuthParams map[string]string // e.g. Google's access_type=offline&prompt=consent
}

// AuthURL builds the URL a human opens to grant access.
func AuthURL(cfg Config, redirectURI string, pkce *PKCE) string {
	q := url.Values{
		"client_id":             {cfg.ClientID},
		"redirect_uri":          {redirectURI},
		"response_type":         {"code"},
		"scope":                 {strings.Join(cfg.Scopes, " ")},
		"code_challenge":        {pkce.Challenge},
		"code_challenge_method": {"S256"},
		"state":                 {pkce.State},
	}
	for k, v := range cfg.ExtraAuthParams {
		q.Set(k, v)
	}
	return cfg.AuthEndpoint + "?" + q.Encode()
}

// TokenResponse mirrors the standard OAuth2 token endpoint response shape.
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"` // only present if the provider issues one
	ExpiresIn    int    `json:"expires_in"`
	TokenType    string `json:"token_type"`
	Scope        string `json:"scope"`
}

// ExchangeCode trades an authorization code for tokens.
func ExchangeCode(cfg Config, code, verifier, redirectURI string) (*TokenResponse, error) {
	form := url.Values{
		"client_id":     {cfg.ClientID},
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"code_verifier": {verifier},
		"redirect_uri":  {redirectURI},
	}
	if cfg.ClientSecret != "" {
		form.Set("client_secret", cfg.ClientSecret)
	}
	return doTokenRequest(cfg.TokenEndpoint, form)
}

// RefreshAccessToken mints a new access token from a stored refresh token.
func RefreshAccessToken(cfg Config, refreshToken string) (*TokenResponse, error) {
	form := url.Values{
		"client_id":     {cfg.ClientID},
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
	}
	if cfg.ClientSecret != "" {
		form.Set("client_secret", cfg.ClientSecret)
	}
	return doTokenRequest(cfg.TokenEndpoint, form)
}

func doTokenRequest(tokenEndpoint string, form url.Values) (*TokenResponse, error) {
	req, err := http.NewRequest(http.MethodPost, tokenEndpoint, bytes.NewReader([]byte(form.Encode())))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("oauth2pkce: token request failed: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("oauth2pkce: token request rejected (%d): %s", resp.StatusCode, truncate(raw))
	}
	var tr TokenResponse
	if err := json.Unmarshal(raw, &tr); err != nil {
		return nil, fmt.Errorf("oauth2pkce: parse token response: %w", err)
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
