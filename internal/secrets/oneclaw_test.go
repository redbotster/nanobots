package secrets

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/redbotster/nanobots/internal/oneclaw"
)

func newOneClawTestServer(t *testing.T, handlers map[string]http.HandlerFunc) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/auth/api-key-token", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"access_token": "test-bearer-token",
			"token_type":   "Bearer",
			"expires_in":   86400,
		})
	})
	for path, h := range handlers {
		mux.HandleFunc(path, h)
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestOneClawStorePutThenGetRoundTrips(t *testing.T) {
	var stored string
	srv := newOneClawTestServer(t, map[string]http.HandlerFunc{
		"/v1/vaults/v1/secrets/slack/bot_token": func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodPut:
				var body oneclaw.PutSecretRequest
				json.NewDecoder(r.Body).Decode(&body)
				stored = body.Value
				w.WriteHeader(http.StatusCreated)
				json.NewEncoder(w).Encode(map[string]any{"id": "s1"})
			case http.MethodGet:
				json.NewEncoder(w).Encode(oneclaw.SecretResponse{ID: "s1", Path: "slack/bot_token", Value: stored})
			}
		},
	})
	client := oneclaw.NewClient("1ck_test")
	client.BaseURL = srv.URL
	store := &OneClaw{Client: client, VaultID: "v1"}

	if err := store.Put("slack/bot_token", "xoxb-abc"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, found, err := store.Get("slack/bot_token")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !found {
		t.Fatal("Get: found=false right after Put")
	}
	if got != "xoxb-abc" {
		t.Fatalf("Get = %q, want xoxb-abc", got)
	}
}

func TestOneClawStoreGetSurfacesTheUnderlyingError(t *testing.T) {
	srv := newOneClawTestServer(t, map[string]http.HandlerFunc{
		"/v1/vaults/v1/secrets/slack/bot_token": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		},
	})
	client := oneclaw.NewClient("1ck_test")
	client.BaseURL = srv.URL
	store := &OneClaw{Client: client, VaultID: "v1"}

	_, found, err := store.Get("slack/bot_token")
	if err == nil {
		t.Fatal("Get: expected an error for a missing secret, got nil")
	}
	if found {
		t.Fatal("Get: found=true alongside a non-nil error")
	}
}

func TestOneClawStoreDeleteIsRefusedNotSilentlyFaked(t *testing.T) {
	store := &OneClaw{Client: oneclaw.NewClient("1ck_test"), VaultID: "v1"}

	if err := store.Delete("slack/bot_token"); err == nil {
		t.Fatal("Delete: expected an error — 1Claw has no per-secret delete, and this must not pretend otherwise")
	}
}
