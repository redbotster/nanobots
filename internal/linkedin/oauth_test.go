package linkedin

import "testing"

func TestOAuthConfigIncludesClientSecret(t *testing.T) {
	cfg := OAuthConfig("client-1", "secret-1")
	if cfg.ClientSecret != "secret-1" {
		t.Error("expected LinkedIn's config to carry a client secret, unlike Google/X")
	}
	if cfg.AuthEndpoint == "" || cfg.TokenEndpoint == "" {
		t.Error("expected real endpoints to be set")
	}
	found := false
	for _, s := range cfg.Scopes {
		if s == "w_member_social" {
			found = true
		}
	}
	if !found {
		t.Error("expected w_member_social in scopes so posting is authorized")
	}
}
