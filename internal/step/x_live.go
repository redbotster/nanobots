package step

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/redbotster/nanobots/internal/oauth2pkce"
	"github.com/redbotster/nanobots/internal/oneclaw"
	"github.com/redbotster/nanobots/internal/x"
)

// XConfig configures direct X (Twitter) access for a service with
// provider: x and a non-demo connection, once an X OAuth "Native App"
// (public) client id exists (X_OAUTH_CLIENT_ID — see internal/x's package
// doc) and the service is switched from `connection: demo` to
// `connection: oauth_native`.
type XConfig struct {
	ClientID        string
	VaultID         string
	RefreshTokenKey string // defaults to "x/refresh_token"
}

func (cfg XConfig) refreshTokenKey() string {
	if cfg.RefreshTokenKey == "" {
		return "x/refresh_token"
	}
	return cfg.RefreshTokenKey
}

func (cfg XConfig) configured() bool {
	return cfg.ClientID != "" && cfg.VaultID != ""
}

// xTokenSource mirrors googleTokenSource exactly: a vault-stored refresh
// token turned into a cached access token, refreshed only once it's within
// a minute of expiring.
type xTokenSource struct {
	oc  *oneclaw.Client
	cfg XConfig

	mu           sync.Mutex
	refreshToken string
	accessToken  string
	expiry       time.Time
}

func (x2 *xTokenSource) Token() (string, error) {
	x2.mu.Lock()
	defer x2.mu.Unlock()
	if x2.accessToken != "" && time.Now().Before(x2.expiry.Add(-1*time.Minute)) {
		return x2.accessToken, nil
	}
	if x2.refreshToken == "" {
		rt, err := x2.oc.GetSecret(x2.cfg.VaultID, x2.cfg.refreshTokenKey())
		if err != nil {
			if locked, ok := oneclaw.AsVaultLocked(err); ok {
				return "", locked
			}
			return "", fmt.Errorf("x: no connected account yet (%w) — connect it from Settings", err)
		}
		x2.refreshToken = rt
	}
	tr, err := oauth2pkce.RefreshAccessToken(x.OAuthConfig(x2.cfg.ClientID), x2.refreshToken)
	if err != nil {
		return "", fmt.Errorf("x: refresh access token: %w", err)
	}
	x2.accessToken = tr.AccessToken
	x2.expiry = time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second)
	return x2.accessToken, nil
}

// xAPI is the subset of *x.Client's methods dispatchX calls — narrow so
// dispatch is unit-testable with a fake instead of a real HTTP server.
type xAPI interface {
	PostTweet(text string) (string, error)
	Mentions(sinceID string, max int) (*x.MentionPage, error)
}

func (l *LiveDeps) xClient() (xAPI, error) {
	if !l.Services.X.configured() {
		return nil, fmt.Errorf("x: not configured — connect an account from Settings")
	}
	if l.xTS == nil {
		l.xTS = &xTokenSource{oc: l.OneClaw, cfg: l.Services.X}
	}
	token, err := l.xTS.Token()
	if err != nil {
		return nil, err
	}
	return x.NewClient(token), nil
}

func dispatchX(c xAPI, op string, params map[string]any) (any, error) {
	switch op {
	case "posts.publish":
		text, _ := params["text"].(string)
		id, err := c.PostTweet(text)
		if err != nil {
			return nil, err
		}
		return map[string]any{"id": id}, nil
	case "mentions.list":
		since, _ := params["since_id"].(string)
		page, err := c.Mentions(since, atoiParam(params["max_results"], 10))
		if err != nil {
			return nil, err
		}
		// A plain []map, not the struct: a service.call result crosses into
		// the interpreter as JSON and a bot's list<json> port has to be able
		// to read the fields by name.
		out := make([]any, 0, len(page.Mentions))
		for _, m := range page.Mentions {
			out = append(out, map[string]any{
				"id": m.ID, "text": m.Text, "author": m.Author, "created_at": m.Created,
			})
		}
		// newest_id is the bookmark bots/x-mentions stores, so the next run
		// asks X only for what came after it. Every read is billed.
		return map[string]any{"mentions": out, "count": len(out), "newest_id": page.NewestID}, nil
	default:
		return nil, fmt.Errorf("x: unsupported op %q", op)
	}
}

// atoiParam reads a numeric step param that may arrive as a string (from a
// bot's `string` input port) or as a JSON number (from a snap). Falling back
// to a default rather than erroring: a malformed page size should not fail
// a run that was going to work.
func atoiParam(v any, def int) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case string:
		if i, err := strconv.Atoi(strings.TrimSpace(n)); err == nil {
			return i
		}
	}
	return def
}
