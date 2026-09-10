package oneclaw

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// newTestServer wires up a fake 1Claw API for the handful of routes this
// package calls, so tests never touch the network or a real account.
func newTestServer(t *testing.T, handlers map[string]http.HandlerFunc) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	for path, h := range handlers {
		mux.HandleFunc(path, h)
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func tokenHandler(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode token request: %v", err)
		}
		if body["api_key"] != "1ck_test" {
			t.Errorf("api_key = %q, want 1ck_test", body["api_key"])
		}
		json.NewEncoder(w).Encode(map[string]any{
			"access_token": "test-bearer-token",
			"token_type":   "Bearer",
			"expires_in":   86400,
		})
	}
}

func TestEnsureTokenExchangesAPIKey(t *testing.T) {
	var sawAuth string
	srv := newTestServer(t, map[string]http.HandlerFunc{
		"/v1/auth/api-key-token": tokenHandler(t),
		"/v1/vaults": func(w http.ResponseWriter, r *http.Request) {
			sawAuth = r.Header.Get("Authorization")
			json.NewEncoder(w).Encode(map[string]any{"vaults": []Vault{}})
		},
	})
	c := NewClient("1ck_test")
	c.BaseURL = srv.URL
	if _, err := c.ListVaults(); err != nil {
		t.Fatalf("ListVaults: %v", err)
	}
	if sawAuth != "Bearer test-bearer-token" {
		t.Errorf("Authorization header = %q, want Bearer test-bearer-token", sawAuth)
	}
}

func TestEnsureVaultIsIdempotent(t *testing.T) {
	createCalls := 0
	srv := newTestServer(t, map[string]http.HandlerFunc{
		"/v1/auth/api-key-token": tokenHandler(t),
		"/v1/vaults": func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost {
				createCalls++
				json.NewEncoder(w).Encode(Vault{ID: "v1", Name: "nanobots-main"})
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"vaults": []Vault{{ID: "v1", Name: "nanobots-main"}}})
		},
	})
	c := NewClient("1ck_test")
	c.BaseURL = srv.URL

	v, err := c.EnsureVault("nanobots-main")
	if err != nil {
		t.Fatalf("EnsureVault: %v", err)
	}
	if v.ID != "v1" {
		t.Errorf("vault id = %q, want v1", v.ID)
	}
	if createCalls != 0 {
		t.Errorf("expected no create call when the vault already exists, got %d", createCalls)
	}
}

func TestEnsureAgentPersistsCredentialAndReusesIt(t *testing.T) {
	createCalls := 0
	srv := newTestServer(t, map[string]http.HandlerFunc{
		"/v1/auth/api-key-token": tokenHandler(t),
		"/v1/agents": func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost {
				createCalls++
				w.WriteHeader(http.StatusCreated)
				json.NewEncoder(w).Encode(map[string]any{
					"agent":   Agent{ID: "a1", Name: "nanobots-recap"},
					"api_key": "ocv_secret",
				})
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"agents": []Agent{}})
		},
	})
	c := NewClient("1ck_test")
	c.BaseURL = srv.URL
	stateDir := t.TempDir()

	id, key, err := c.EnsureAgent(stateDir, "nanobots-recap", CreateAgentRequest{ShroudEnabled: true})
	if err != nil {
		t.Fatalf("EnsureAgent (create): %v", err)
	}
	if id != "a1" || key != "ocv_secret" {
		t.Fatalf("EnsureAgent = %q, %q, want a1, ocv_secret", id, key)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "nanobots-recap.json")); err != nil {
		t.Fatalf("expected a persisted credential file: %v", err)
	}

	// Second call must reuse the saved credential, not hit /v1/agents POST again.
	id2, key2, err := c.EnsureAgent(stateDir, "nanobots-recap", CreateAgentRequest{})
	if err != nil {
		t.Fatalf("EnsureAgent (reuse): %v", err)
	}
	if id2 != id || key2 != key {
		t.Errorf("EnsureAgent (reuse) = %q, %q, want %q, %q", id2, key2, id, key)
	}
	if createCalls != 1 {
		t.Errorf("expected exactly 1 create call, got %d", createCalls)
	}
}

func TestEnsureAgentErrorsWhenRemoteExistsButCredentialLost(t *testing.T) {
	srv := newTestServer(t, map[string]http.HandlerFunc{
		"/v1/auth/api-key-token": tokenHandler(t),
		"/v1/agents": func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(map[string]any{"agents": []Agent{{ID: "a1", Name: "nanobots-recap"}}})
		},
	})
	c := NewClient("1ck_test")
	c.BaseURL = srv.URL
	_, _, err := c.EnsureAgent(t.TempDir(), "nanobots-recap", CreateAgentRequest{})
	if err == nil {
		t.Fatal("expected an error when the agent exists remotely but no local credential is saved")
	}
}

func TestExecuteApprovalRequired(t *testing.T) {
	srv := newTestServer(t, map[string]http.HandlerFunc{
		"/v1/auth/api-key-token": tokenHandler(t),
		"/v1/agents/a1/execute": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusAccepted)
			json.NewEncoder(w).Encode(map[string]any{
				"error":       "approval_required",
				"approval_id": "appr-1",
				"status":      "pending",
			})
		},
	})
	c := NewClient("1ck_test")
	c.BaseURL = srv.URL
	_, err := c.Execute("a1", "gmail", "http", map[string]any{})
	if err == nil {
		t.Fatal("expected an approval-required error")
	}
	var approvalErr *ApprovalRequiredError
	if !errors.As(err, &approvalErr) {
		t.Fatalf("error was not *ApprovalRequiredError: %v", err)
	}
	if approvalErr.ApprovalID != "appr-1" {
		t.Errorf("ApprovalID = %q, want appr-1", approvalErr.ApprovalID)
	}
}
