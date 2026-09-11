// Package x implements real X (Twitter) posting via a standard OAuth2 +
// PKCE flow — bots/post-publisher's `provider: x` service, which runs on
// `connection: demo` until a human connects a real account (see
// docs/connections.md). Assumes a "public client" (installed-app) X OAuth
// 2.0 app — no client secret, PKCE only — the same app type Google's
// Desktop-app flow uses, registered by the user at
// developer.x.com/en/portal/dashboard with the "Native App" (public
// client) type and the "tweet.read", "tweet.write", "users.read", and
// "offline.access" scopes (the last one is what makes X return a refresh
// token, so the connection outlives one 2-hour access token).
package x

import (
	"github.com/redbotster/nanobots/internal/oauth2pkce"
	"github.com/redbotster/nanobots/internal/oneclaw"
)

const (
	authEndpoint  = "https://x.com/i/oauth2/authorize"
	tokenEndpoint = "https://api.x.com/2/oauth2/token"
)

// DefaultScopes covers everything post-publisher needs: reading the
// connected account's own identity isn't required for posting, but
// offline.access is what makes the refresh token show up at all.
var DefaultScopes = []string{"tweet.read", "tweet.write", "users.read", "offline.access"}

func OAuthConfig(clientID string) oauth2pkce.Config {
	return oauth2pkce.Config{
		AuthEndpoint:  authEndpoint,
		TokenEndpoint: tokenEndpoint,
		ClientID:      clientID,
		Scopes:        DefaultScopes,
	}
}

// LoadClientID reads X_OAUTH_CLIENT_ID from the same dotenv-style file as
// ONECLAW_API_KEY (path="" uses oneclaw.DefaultEnvFilePath). Returns ("",
// nil) if it isn't set yet.
func LoadClientID(path string) (string, error) {
	return oneclaw.LoadEnvValue(path, "X_OAUTH_CLIENT_ID")
}
