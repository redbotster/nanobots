package google

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

func TestAuthURLIncludesRequiredParams(t *testing.T) {
	pkce := &PKCE{Verifier: "v", Challenge: "c", State: "s"}
	raw := AuthURL("client-123", "http://127.0.0.1:9999/", []string{ScopeGmailReadonly, ScopeDriveFile}, pkce)
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
		"prompt":                "consent",
	} {
		if got := q.Get(key); got != want {
			t.Errorf("query param %s = %q, want %q", key, got, want)
		}
	}
	if !strings.Contains(q.Get("scope"), "gmail.readonly") {
		t.Errorf("scope missing gmail.readonly: %q", q.Get("scope"))
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

	orig := tokenEndpoint
	tokenEndpoint = srv.URL
	defer func() { tokenEndpoint = orig }()

	tok, err := ExchangeCode("client-1", "code-1", "verifier-1", "http://127.0.0.1:1/")
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	if tok.AccessToken != "access-1" || tok.RefreshToken != "refresh-1" {
		t.Errorf("token = %+v", tok)
	}
	if gotForm.Get("grant_type") != "authorization_code" || gotForm.Get("code_verifier") != "verifier-1" {
		t.Errorf("form = %v", gotForm)
	}
}

func TestBuildRFC2822IsValidBase64URL(t *testing.T) {
	raw := buildRFC2822("someone@example.com", "Hello", "Body text")
	if strings.ContainsAny(raw, "+/=") {
		t.Errorf("expected base64url (no +, /, or = padding), got %q", raw)
	}
}
