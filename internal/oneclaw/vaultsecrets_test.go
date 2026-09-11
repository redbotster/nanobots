package oneclaw

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestPutSecretThenGetSecretRoundTrips(t *testing.T) {
	var stored string
	srv := newTestServer(t, map[string]http.HandlerFunc{
		"/v1/auth/api-key-token": tokenHandler(t),
		"/v1/vaults/v1/secrets/google/refresh_token": func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodPut:
				var body PutSecretRequest
				json.NewDecoder(r.Body).Decode(&body)
				stored = body.Value
				w.WriteHeader(http.StatusCreated)
				json.NewEncoder(w).Encode(map[string]any{"id": "s1", "path": "google/refresh_token", "type": "generic", "version": 1})
			case http.MethodGet:
				json.NewEncoder(w).Encode(SecretResponse{ID: "s1", Path: "google/refresh_token", Type: "generic", Value: stored, Version: 1})
			}
		},
	})
	c := NewClient("1ck_test")
	c.BaseURL = srv.URL

	if err := c.PutSecret("v1", "google/refresh_token", "1//rt-abc123"); err != nil {
		t.Fatalf("PutSecret: %v", err)
	}
	got, err := c.GetSecret("v1", "google/refresh_token")
	if err != nil {
		t.Fatalf("GetSecret: %v", err)
	}
	if got != "1//rt-abc123" {
		t.Errorf("GetSecret = %q, want 1//rt-abc123", got)
	}
}

func TestGetSecretMissingReturnsError(t *testing.T) {
	srv := newTestServer(t, map[string]http.HandlerFunc{
		"/v1/auth/api-key-token": tokenHandler(t),
		"/v1/vaults/v1/secrets/google/refresh_token": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		},
	})
	c := NewClient("1ck_test")
	c.BaseURL = srv.URL

	if _, err := c.GetSecret("v1", "google/refresh_token"); err == nil {
		t.Fatal("expected an error for a missing secret, got nil")
	}
}
