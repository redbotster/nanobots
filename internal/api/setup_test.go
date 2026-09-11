package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/redbotster/nanobots/internal/oneclaw"
)

func TestHandleSetupOneClawKeyRejectsEmptyKey(t *testing.T) {
	srv := testServer(t)
	srv.EnvFilePath = filepath.Join(t.TempDir(), "nanobots.env")
	body, _ := json.Marshal(setupOneClawKeyRequest{APIKey: "  "})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/setup/oneclaw-key", bytes.NewReader(body)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleSetupOneClawKeyRejectsAKeyThatDoesntAuthenticate(t *testing.T) {
	// A fake server that rejects every key, exactly how the real 1Claw API
	// would for a wrong one — no live network call in a unit test.
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/auth/api-key-token", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"invalid api key"}`))
	})
	fakeSrv := httptest.NewServer(mux)
	t.Cleanup(fakeSrv.Close)
	orig := oneclaw.DefaultBaseURL
	oneclaw.DefaultBaseURL = fakeSrv.URL
	t.Cleanup(func() { oneclaw.DefaultBaseURL = orig })

	srv := testServer(t)
	envPath := filepath.Join(t.TempDir(), "nanobots.env")
	srv.EnvFilePath = envPath
	body, _ := json.Marshal(setupOneClawKeyRequest{APIKey: "1ck_definitely_wrong"})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/setup/oneclaw-key", bytes.NewReader(body)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body: %s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(envPath); !os.IsNotExist(err) {
		t.Error("expected no file to be written for a key that failed to authenticate")
	}
}

func TestHandleSetupOneClawKeySavesAWorkingKey(t *testing.T) {
	srv := testServer(t)
	// handleSetupOneClawKey builds its own throwaway oneclaw.Client (the
	// whole point is validating a *new* key before anything else trusts
	// it, so it can't reuse srv.OneClaw) — point every oneclaw.Client this
	// process creates at a fake server for the duration of this test via
	// DefaultBaseURL, the same override compose_test.go's
	// testServerForCompose already does for DefaultShroudURL.
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/auth/api-key-token", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"access_token": "tok", "token_type": "Bearer", "expires_in": 86400})
	})
	mux.HandleFunc("/v1/agents", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"agents": []map[string]string{}})
	})
	fakeSrv := httptest.NewServer(mux)
	t.Cleanup(fakeSrv.Close)
	orig := oneclaw.DefaultBaseURL
	oneclaw.DefaultBaseURL = fakeSrv.URL
	t.Cleanup(func() { oneclaw.DefaultBaseURL = orig })

	envPath := filepath.Join(t.TempDir(), "nanobots.env")
	srv.EnvFilePath = envPath

	body, _ := json.Marshal(setupOneClawKeyRequest{APIKey: "1ck_test"})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/setup/oneclaw-key", bytes.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}
	raw, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("read env file: %v", err)
	}
	if got, err := oneclaw.LoadEnvValue(envPath, "ONECLAW_API_KEY"); err != nil || got != "1ck_test" {
		t.Errorf("saved key = %q, %v; raw file:\n%s", got, err, raw)
	}
}
