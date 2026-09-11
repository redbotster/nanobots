package oauth2pkce

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestNewPKCEProducesDistinctValues(t *testing.T) {
	a, err := NewPKCE()
	if err != nil {
		t.Fatalf("NewPKCE: %v", err)
	}
	b, err := NewPKCE()
	if err != nil {
		t.Fatalf("NewPKCE: %v", err)
	}
	if a.Verifier == b.Verifier || a.State == b.State {
		t.Error("expected two PKCE attempts to never reuse verifier/state")
	}
	if a.Verifier == a.Challenge {
		t.Error("challenge must be a transform of the verifier, not the verifier itself")
	}
}

func TestAuthURLIncludesRequiredParamsAndExtras(t *testing.T) {
	pkce := &PKCE{Verifier: "v", Challenge: "c", State: "s"}
	cfg := Config{
		AuthEndpoint: "https://example.com/authorize",
		ClientID:     "client-123",
		Scopes:       []string{"scope.a", "scope.b"},
		ExtraAuthParams: map[string]string{
			"access_type": "offline",
		},
	}
	raw := AuthURL(cfg, "http://127.0.0.1:9999/", pkce)
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("AuthURL produced an invalid URL: %v", err)
	}
	q := u.Query()
	for key, want := range map[string]string{
		"client_id":             "client-123",
		"redirect_uri":          "http://127.0.0.1:9999/",
		"response_type":         "code",
		"code_challenge":        "c",
		"code_challenge_method": "S256",
		"state":                 "s",
		"access_type":           "offline",
	} {
		if got := q.Get(key); got != want {
			t.Errorf("query param %s = %q, want %q", key, got, want)
		}
	}
	if !strings.Contains(q.Get("scope"), "scope.a") {
		t.Errorf("scope missing scope.a: %q", q.Get("scope"))
	}
}

func TestExchangeCodeSendsCorrectFormAndParsesResponse(t *testing.T) {
	var gotForm url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		gotForm = r.PostForm
		json.NewEncoder(w).Encode(TokenResponse{
			AccessToken: "access-1", RefreshToken: "refresh-1", ExpiresIn: 3600, TokenType: "Bearer",
		})
	}))
	defer srv.Close()

	cfg := Config{TokenEndpoint: srv.URL, ClientID: "client-1"}
	tok, err := ExchangeCode(cfg, "code-1", "verifier-1", "http://127.0.0.1:1/")
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	if tok.AccessToken != "access-1" || tok.RefreshToken != "refresh-1" {
		t.Errorf("token = %+v", tok)
	}
	if gotForm.Get("grant_type") != "authorization_code" || gotForm.Get("code_verifier") != "verifier-1" {
		t.Errorf("form = %v", gotForm)
	}
	if gotForm.Get("client_secret") != "" {
		t.Error("expected no client_secret in the form when Config.ClientSecret is empty")
	}
}

func TestExchangeCodeIncludesClientSecretWhenConfigured(t *testing.T) {
	var gotForm url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		gotForm = r.PostForm
		json.NewEncoder(w).Encode(TokenResponse{AccessToken: "a", ExpiresIn: 60})
	}))
	defer srv.Close()

	cfg := Config{TokenEndpoint: srv.URL, ClientID: "client-1", ClientSecret: "shh"}
	if _, err := ExchangeCode(cfg, "code-1", "verifier-1", "http://127.0.0.1:1/"); err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	if gotForm.Get("client_secret") != "shh" {
		t.Errorf("expected client_secret to be sent, form = %v", gotForm)
	}
}

func TestRefreshAccessTokenSendsRefreshGrant(t *testing.T) {
	var gotForm url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		gotForm = r.PostForm
		json.NewEncoder(w).Encode(TokenResponse{AccessToken: "fresh", ExpiresIn: 3600})
	}))
	defer srv.Close()

	cfg := Config{TokenEndpoint: srv.URL, ClientID: "client-1"}
	tok, err := RefreshAccessToken(cfg, "refresh-tok")
	if err != nil {
		t.Fatalf("RefreshAccessToken: %v", err)
	}
	if tok.AccessToken != "fresh" {
		t.Errorf("token = %+v", tok)
	}
	if gotForm.Get("grant_type") != "refresh_token" || gotForm.Get("refresh_token") != "refresh-tok" {
		t.Errorf("form = %v", gotForm)
	}
}

func TestDoTokenRequestSurfacesNonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"invalid_client"}`))
	}))
	defer srv.Close()

	cfg := Config{TokenEndpoint: srv.URL, ClientID: "client-1"}
	if _, err := ExchangeCode(cfg, "code", "verifier", "http://127.0.0.1:1/"); err == nil {
		t.Fatal("expected an error on a non-200 token response")
	}
}
