package linkedin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestUserInfoParsesSubIntoURN(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer tok" {
			t.Errorf("Authorization = %q", got)
		}
		json.NewEncoder(w).Encode(map[string]string{"sub": "abc123"})
	}))
	defer srv.Close()

	c := NewClient("tok")
	c.BaseURL = srv.URL
	urn, err := c.UserInfo()
	if err != nil {
		t.Fatalf("UserInfo: %v", err)
	}
	if urn != "urn:li:person:abc123" {
		t.Errorf("urn = %q", urn)
	}
}

func TestUserInfoSurfacesMissingSub(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{})
	}))
	defer srv.Close()

	c := NewClient("tok")
	c.BaseURL = srv.URL
	if _, err := c.UserInfo(); err == nil {
		t.Fatal("expected an error when the response has no sub claim")
	}
}

func TestPostShareReadsIDFromHeader(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&gotBody)
		if got := r.Header.Get("X-Restli-Protocol-Version"); got != "2.0.0" {
			t.Errorf("X-Restli-Protocol-Version = %q", got)
		}
		w.Header().Set("X-RestLi-Id", "urn:li:share:999")
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	c := NewClient("tok")
	c.BaseURL = srv.URL
	id, err := c.PostShare("urn:li:person:abc123", "hello linkedin")
	if err != nil {
		t.Fatalf("PostShare: %v", err)
	}
	if id != "urn:li:share:999" {
		t.Errorf("id = %q", id)
	}
	if gotBody["author"] != "urn:li:person:abc123" {
		t.Errorf("author = %v", gotBody["author"])
	}
}

func TestPostShareSurfacesNonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"message":"forbidden"}`))
	}))
	defer srv.Close()

	c := NewClient("tok")
	c.BaseURL = srv.URL
	if _, err := c.PostShare("urn:li:person:abc123", "hi"); err == nil {
		t.Fatal("expected an error on a 403 response")
	}
}
