// Package linkedin implements real LinkedIn posting via OAuth2 + PKCE —
// bots/post-publisher's `provider: linkedin` service, which runs on
// `connection: demo` until a human connects a real account (see
// docs/connections.md). Unlike Google or X, LinkedIn's OAuth2
// implementation doesn't support a true public client: it requires a
// client secret on the token exchange even with PKCE also in play, and by
// default (without LinkedIn's separate "Programmatic Refresh Tokens"
// product access) issues an access token with no refresh token at all —
// good for about 60 days, after which the human has to reconnect. Both are
// real LinkedIn platform constraints, not something worked around here.
package linkedin

import (
	"github.com/redbotster/nanobots/internal/oauth2pkce"
	"github.com/redbotster/nanobots/internal/oneclaw"
)

const (
	authEndpoint  = "https://www.linkedin.com/oauth/v2/authorization"
	tokenEndpoint = "https://www.linkedin.com/oauth/v2/accessToken"
)

// DefaultScopes: w_member_social to post, openid+profile to resolve the
// connected member's own URN (posting requires "author": "urn:li:person:
// <id>", and LinkedIn only hands that out via its OpenID Connect userinfo
// endpoint, not the OAuth token response itself).
var DefaultScopes = []string{"openid", "profile", "w_member_social"}

func OAuthConfig(clientID, clientSecret string) oauth2pkce.Config {
	return oauth2pkce.Config{
		AuthEndpoint:  authEndpoint,
		TokenEndpoint: tokenEndpoint,
		ClientID:      clientID,
		ClientSecret:  clientSecret,
		Scopes:        DefaultScopes,
	}
}

// LoadClientID/LoadClientSecret read LINKEDIN_OAUTH_CLIENT_ID/_SECRET from
// the same dotenv-style file as ONECLAW_API_KEY (path="" uses
// oneclaw.DefaultEnvFilePath). Return ("", nil) if unset.
func LoadClientID(path string) (string, error) {
	return oneclaw.LoadEnvValue(path, "LINKEDIN_OAUTH_CLIENT_ID")
}

func LoadClientSecret(path string) (string, error) {
	return oneclaw.LoadEnvValue(path, "LINKEDIN_OAUTH_CLIENT_SECRET")
}
