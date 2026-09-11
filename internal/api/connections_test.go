package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/redbotster/nanobots/internal/oneclaw"
)

// fakeOneClaw wires up just enough of the real 1Claw Human API — token
// exchange, vault ensure, and secret put/get, backed by an in-memory map —
// for the connections endpoints to exercise a real oneclaw.Client against,
// without a real account.
func fakeOneClaw(t *testing.T) *oneclaw.Client {
	t.Helper()
	secrets := map[string]string{}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/auth/api-key-token", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"access_token": "tok", "token_type": "Bearer", "expires_in": 86400})
	})
	mux.HandleFunc("/v1/vaults", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			json.NewEncoder(w).Encode(map[string]string{"id": "v1", "name": "nanobots-main"})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"vaults": []map[string]string{{"id": "v1", "name": "nanobots-main"}}})
	})
	mux.HandleFunc("/v1/vaults/v1/secrets/", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path[len("/v1/vaults/v1/secrets/"):]
		switch r.Method {
		case http.MethodPut:
			var body map[string]string
			json.NewDecoder(r.Body).Decode(&body)
			secrets[path] = body["value"]
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]any{"id": "s1", "path": path, "type": "generic", "version": 1})
		case http.MethodGet:
			val, ok := secrets[path]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"id": "s1", "path": path, "type": "generic", "value": val, "version": 1})
		}
	})
	mux.HandleFunc("/v1/agents", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			json.NewEncoder(w).Encode(map[string]any{
				"agent":   map[string]any{"id": "agent-1", "name": body["name"]},
				"api_key": "ocv_test",
			})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"agents": []map[string]string{}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	oc := oneclaw.NewClient("1ck_test")
	oc.BaseURL = srv.URL
	return oc
}

func testServerWithOneClaw(t *testing.T) *Server {
	t.Helper()
	srv := testServer(t)
	srv.OneClaw = fakeOneClaw(t)
	return srv
}

func TestHandleConnectionsStatusEmptyWhenOneClawNotConfigured(t *testing.T) {
	srv := testServer(t) // no OneClaw set
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/connections", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
	}
	var statuses []connectionStatus
	json.Unmarshal(rec.Body.Bytes(), &statuses)
	if len(statuses) != 0 {
		t.Errorf("statuses = %+v, want empty", statuses)
	}
}

func TestHandleConnectionsStatusReflectsWhatsInTheVault(t *testing.T) {
	srv := testServerWithOneClaw(t)

	// Before connecting anything, everything's false.
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/connections", nil))
	var before []connectionStatus
	json.Unmarshal(rec.Body.Bytes(), &before)
	for _, s := range before {
		if s.Connected {
			t.Errorf("expected %s to start disconnected, got %+v", s.Service, s)
		}
	}

	// Connect Slack via a pasted token.
	body, _ := json.Marshal(connectTokenRequest{Token: "xoxb-abc"})
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/connections/slack", bytes.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("connect slack: status = %d, body: %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/connections", nil))
	var after []connectionStatus
	json.Unmarshal(rec.Body.Bytes(), &after)
	found := false
	for _, s := range after {
		if s.Service == "slack" {
			found = true
			if !s.Connected {
				t.Error("expected slack to be connected after posting a token")
			}
		}
		if s.Service == "github" && s.Connected {
			t.Error("expected github to remain disconnected")
		}
	}
	if !found {
		t.Fatal("expected a slack entry in the status list")
	}
}

func TestHandleConnectTokenRejectsEmptyToken(t *testing.T) {
	srv := testServerWithOneClaw(t)
	body, _ := json.Marshal(connectTokenRequest{Token: "  "})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/connections/github", bytes.NewReader(body)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleConnectTokenRejectsUnknownService(t *testing.T) {
	srv := testServerWithOneClaw(t)
	body, _ := json.Marshal(connectTokenRequest{Token: "x"})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/connections/dropbox", bytes.NewReader(body)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleConnectTokenRejectsGoogleAsAPastedToken(t *testing.T) {
	// Google is a refresh-token/OAuth flow, not a static paste-a-token one —
	// must go through /api/connections/google/start instead.
	srv := testServerWithOneClaw(t)
	body, _ := json.Marshal(connectTokenRequest{Token: "x"})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/connections/google", bytes.NewReader(body)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleConnectGoogleStartRejectsWhenNoClientIDConfigured(t *testing.T) {
	t.Setenv("NANOBOTS_ENV_FILE", "/nonexistent/path/for/this/test.env")
	srv := testServerWithOneClaw(t)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/connections/google/start", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body: %s", rec.Code, rec.Body.String())
	}
}
