package x

import "testing"

func TestOAuthConfigHasNoClientSecret(t *testing.T) {
	cfg := OAuthConfig("client-1")
	if cfg.ClientID != "client-1" {
		t.Errorf("ClientID = %q", cfg.ClientID)
	}
	if cfg.ClientSecret != "" {
		t.Error("expected a public-client config with no client secret")
	}
	if cfg.AuthEndpoint == "" || cfg.TokenEndpoint == "" {
		t.Error("expected real endpoints to be set")
	}
	found := false
	for _, s := range cfg.Scopes {
		if s == "offline.access" {
			found = true
		}
	}
	if !found {
		t.Error("expected offline.access in scopes so a refresh token comes back")
	}
}
