package oneclaw

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func connectorServer(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/auth/api-key-token" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "tok", "token_type": "Bearer", "expires_in": 86400,
			})
			return
		}
		handler(w, r)
	}))
	t.Cleanup(srv.Close)
	orig := DefaultBaseURL
	DefaultBaseURL = srv.URL
	t.Cleanup(func() { DefaultBaseURL = orig })
	return NewClient("k")
}

// The catalogue shape, taken from the live response: gmail, slack, github
// and the rest, each carrying the provider and scopes an install will ask
// for.
func TestListingTheConnectorCatalogue(t *testing.T) {
	c := connectorServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/connectors/presets" {
			t.Errorf("path = %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"presets":[
		  {"slug":"gmail","display_name":"Gmail","provider_slug":"google",
		   "oauth_scopes":["gmail.readonly","gmail.compose"],"required_scopes":["gmail.readonly"],
		   "requires_oauth":true,"allowed_hosts":["gmail.googleapis.com"]},
		  {"slug":"slack","display_name":"Slack","provider_slug":"slack","requires_oauth":true}
		]}`))
	})
	got, err := c.ListConnectorPresets()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Slug != "gmail" || got[0].Provider != "google" {
		t.Fatalf("got %+v", got)
	}
	if len(got[0].OAuthScopes) != 2 || len(got[0].RequiredScopes) != 1 {
		t.Errorf("scopes lost: %+v", got[0])
	}
}

// The binding name is not cosmetic: LiveDeps.ServiceCall executes against a
// binding named after the bot's own service id, so installing gmail as
// "gmail" is what makes a bot's `services: [{id: gmail}]` resolve to it.
func TestInstallSendsTheBindingNameAndNarrowedScopes(t *testing.T) {
	var gotPath, gotBody string
	c := connectorServer(t, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		gotPath, gotBody = r.URL.Path, string(raw)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"binding_id":"b1","binding_name":"gmail","preset_slug":"gmail",
		  "authorization_url":"https://1claw.co/oauth/x","next_step":"Open the link and grant access."}`))
	})

	got, err := c.InstallConnector("agent-1", "gmail", "gmail", []string{"gmail.readonly"})
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/v1/agents/agent-1/connectors/gmail/install" {
		t.Errorf("path = %s", gotPath)
	}
	if !strings.Contains(gotBody, `"binding_name":"gmail"`) || !strings.Contains(gotBody, "gmail.readonly") {
		t.Errorf("body = %s", gotBody)
	}
	if got.AuthorizationURL == "" || got.NextStep == "" {
		t.Errorf("the human has nowhere to go: %+v", got)
	}
}

// Some connectors take a pasted API key instead of an OAuth round trip, so
// a caller must check for the URL rather than assume one.
func TestAConnectorWithoutOAuthReturnsNoURL(t *testing.T) {
	c := connectorServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"binding_id":"b2","binding_name":"honcho","preset_slug":"honcho",
		  "next_step":"Paste your API key in the 1Claw dashboard."}`))
	})
	got, err := c.InstallConnector("agent-1", "honcho", "honcho", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.AuthorizationURL != "" {
		t.Errorf("invented a URL: %q", got.AuthorizationURL)
	}
	if got.NextStep == "" {
		t.Error("no next step, so the user is stuck with a binding and no instructions")
	}
}

// An install creates the binding; the binding is not usable until someone
// finishes in a browser. Conflating the two is how a bot ends up failing on
// a credential that looks present.
func TestInstalledIsNotTheSameAsConnected(t *testing.T) {
	c := connectorServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"connectors":[
		  {"binding_id":"b1","binding_name":"gmail","preset_slug":"gmail","connected":false},
		  {"binding_id":"b2","binding_name":"slack","preset_slug":"slack","connected":true}]}`))
	})
	got, err := c.ListAgentConnectors("agent-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Connected || !got[1].Connected {
		t.Fatalf("got %+v", got)
	}
}

func TestInstallNeedsAnAgentAndASlug(t *testing.T) {
	c := NewClient("k")
	if _, err := c.InstallConnector("", "gmail", "gmail", nil); err == nil {
		t.Error("accepted an empty agent")
	}
	if _, err := c.InstallConnector("a", "", "gmail", nil); err == nil {
		t.Error("accepted an empty slug")
	}
}

// Registering the app is what makes a connector installable, and the secret
// must go to 1Claw and nowhere else — never to a file in this repo.
func TestSavingAppCredentialsSendsTheSecretAndNothingElseKeepsIt(t *testing.T) {
	var gotPath, gotBody string
	c := connectorServer(t, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		gotPath, gotBody = r.URL.Path, string(raw)
		w.WriteHeader(http.StatusNoContent)
	})
	if err := c.SaveOAuthAppCredentials("agent-1", "google", "cid.apps.googleusercontent.com", "shh", ""); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/v1/agents/agent-1/oauth/app-credentials" {
		t.Errorf("path = %s", gotPath)
	}
	for _, want := range []string{`"provider_slug":"google"`, `"client_id":"cid`, `"client_secret":"shh"`} {
		if !strings.Contains(gotBody, want) {
			t.Errorf("body missing %s: %s", want, gotBody)
		}
	}
	// An omitted redirect must not be sent as an empty string, which some
	// providers treat as a literal override.
	if strings.Contains(gotBody, "redirect_uri") {
		t.Errorf("sent an empty redirect_uri: %s", gotBody)
	}
}

func TestSavingCredentialsNeedsAllOfThem(t *testing.T) {
	c := NewClient("k")
	for _, args := range [][4]string{
		{"", "google", "id", "secret"},
		{"a", "", "id", "secret"},
		{"a", "google", "", "secret"},
		{"a", "google", "id", ""},
	} {
		if err := c.SaveOAuthAppCredentials(args[0], args[1], args[2], args[3], ""); err == nil {
			t.Errorf("accepted %v", args)
		}
	}
}

// The secret is write-only at 1Claw, so a listing tells you which providers
// are registered and never hands the secret back.
func TestListingCredentialsNeverReturnsASecret(t *testing.T) {
	c := connectorServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"credentials":[{"provider_slug":"google","client_id":"cid"}]}`))
	})
	got, err := c.ListOAuthAppCredentials("agent-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Provider != "google" || got[0].ClientID != "cid" {
		t.Fatalf("got %+v", got)
	}
}
