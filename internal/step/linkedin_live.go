package step

import (
	"fmt"
	"sync"
	"time"

	"github.com/redbotster/nanobots/internal/linkedin"
	"github.com/redbotster/nanobots/internal/oauth2pkce"
	"github.com/redbotster/nanobots/internal/oneclaw"
)

// LinkedInConfig configures direct LinkedIn access for a service with
// provider: linkedin and a non-demo connection. Unlike Google/X, LinkedIn
// needs a client secret too (see internal/linkedin's package doc) and may
// never hand out a refresh token — see linkedinTokenSource below for how
// that's handled honestly rather than assumed away.
type LinkedInConfig struct {
	ClientID        string
	ClientSecret    string
	VaultID         string
	RefreshTokenKey string // defaults to "linkedin/refresh_token"
	AccessTokenKey  string // defaults to "linkedin/access_token" — used when there's no refresh token
}

func (cfg LinkedInConfig) refreshTokenKey() string {
	if cfg.RefreshTokenKey == "" {
		return "linkedin/refresh_token"
	}
	return cfg.RefreshTokenKey
}

func (cfg LinkedInConfig) accessTokenKey() string {
	if cfg.AccessTokenKey == "" {
		return "linkedin/access_token"
	}
	return cfg.AccessTokenKey
}

func (cfg LinkedInConfig) configured() bool {
	return cfg.ClientID != "" && cfg.VaultID != ""
}

// linkedinTokenSource prefers a stored refresh token (refreshed like
// Google/X's) but falls back to a stored long-lived access token used
// as-is when LinkedIn never issued a refresh token for this app — LinkedIn
// only does that with its separate "Programmatic Refresh Tokens" product
// access, which isn't something this build can assume every connected app
// has. In the fallback case there's nothing to refresh: once LinkedIn
// rejects the access token (it's typically valid ~60 days), the human has
// to reconnect from Settings.
type linkedinTokenSource struct {
	oc  *oneclaw.Client
	cfg LinkedInConfig

	mu           sync.Mutex
	refreshToken string
	accessToken  string
	expiry       time.Time
	staticOnly   bool
}

func (ts *linkedinTokenSource) Token() (string, error) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if ts.staticOnly && ts.accessToken != "" {
		return ts.accessToken, nil
	}
	if ts.accessToken != "" && time.Now().Before(ts.expiry.Add(-1*time.Minute)) {
		return ts.accessToken, nil
	}
	if ts.refreshToken == "" && !ts.staticOnly {
		rt, err := ts.oc.GetSecret(ts.cfg.VaultID, ts.cfg.refreshTokenKey())
		if err != nil {
			at, aerr := ts.oc.GetSecret(ts.cfg.VaultID, ts.cfg.accessTokenKey())
			if aerr != nil {
				if locked, ok := oneclaw.AsVaultLocked(err); ok {
					return "", locked
				}
				return "", fmt.Errorf("linkedin: no connected account yet (%w) — connect it from Settings", err)
			}
			ts.staticOnly = true
			ts.accessToken = at
			return ts.accessToken, nil
		}
		ts.refreshToken = rt
	}
	tr, err := oauth2pkce.RefreshAccessToken(linkedin.OAuthConfig(ts.cfg.ClientID, ts.cfg.ClientSecret), ts.refreshToken)
	if err != nil {
		return "", fmt.Errorf("linkedin: refresh access token: %w", err)
	}
	ts.accessToken = tr.AccessToken
	ts.expiry = time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second)
	return ts.accessToken, nil
}

// linkedinAPI is the subset of *linkedin.Client's methods dispatchLinkedIn
// calls — narrow so dispatch is unit-testable with a fake.
type linkedinAPI interface {
	UserInfo() (string, error)
	PostShare(personURN, text string) (string, error)
}

func (l *LiveDeps) linkedinClient() (linkedinAPI, error) {
	if !l.Services.LinkedIn.configured() {
		return nil, fmt.Errorf("linkedin: not configured — connect an account from Settings")
	}
	if l.linkedinTS == nil {
		l.linkedinTS = &linkedinTokenSource{oc: l.OneClaw, cfg: l.Services.LinkedIn}
	}
	token, err := l.linkedinTS.Token()
	if err != nil {
		return nil, err
	}
	return linkedin.NewClient(token), nil
}

func dispatchLinkedIn(c linkedinAPI, op string, params map[string]any) (any, error) {
	switch op {
	case "posts.publish":
		text, _ := params["text"].(string)
		urn, err := c.UserInfo()
		if err != nil {
			return nil, err
		}
		id, err := c.PostShare(urn, text)
		if err != nil {
			return nil, err
		}
		return map[string]any{"id": id}, nil
	default:
		return nil, fmt.Errorf("linkedin: unsupported op %q", op)
	}
}
