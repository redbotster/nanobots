package oneclaw

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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
	// The listing has to reflect creates, the way the real API does —
	// EnsureAgent now checks a saved credential against it, so a fake that
	// always answers "no agents" is a fake that models nothing.
	var existing []Agent
	srv := newTestServer(t, map[string]http.HandlerFunc{
		"/v1/auth/api-key-token": tokenHandler(t),
		"/v1/agents": func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost {
				createCalls++
				existing = append(existing, Agent{ID: "a1", Name: "nanobots-recap"})
				w.WriteHeader(http.StatusCreated)
				json.NewEncoder(w).Encode(map[string]any{
					"agent":   Agent{ID: "a1", Name: "nanobots-recap"},
					"api_key": "ocv_secret",
				})
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"agents": existing})
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

// The bug this fixes, hit for real: seven nanobots-* agents were deleted on
// 1Claw, their credential files stayed behind in ~/.nanobots/state/agents/,
// and every run of those bots then died on `shroud: chat failed (401): agent
// key exchange failed` — a message that gives no hint the fix is "delete a
// local file". EnsureAgent now notices the agent is gone and makes a new one.
func TestEnsureAgentReplacesACredentialWhoseAgentWasDeleted(t *testing.T) {
	createCalls := 0
	var existing []Agent
	srv := newTestServer(t, map[string]http.HandlerFunc{
		"/v1/auth/api-key-token": tokenHandler(t),
		"/v1/agents": func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost {
				createCalls++
				id := fmt.Sprintf("a%d", createCalls)
				existing = append(existing, Agent{ID: id, Name: "nanobots-recap"})
				w.WriteHeader(http.StatusCreated)
				json.NewEncoder(w).Encode(map[string]any{
					"agent":   Agent{ID: id, Name: "nanobots-recap"},
					"api_key": "ocv_" + id,
				})
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"agents": existing})
		},
	})
	c := NewClient("1ck_test")
	c.BaseURL = srv.URL
	stateDir := t.TempDir()

	if _, _, err := c.EnsureAgent(stateDir, "nanobots-recap", CreateAgentRequest{}); err != nil {
		t.Fatalf("first EnsureAgent: %v", err)
	}

	// Someone deletes it on 1Claw. The local credential file is untouched.
	existing = nil
	c.invalidateAgentCache()

	id, key, err := c.EnsureAgent(stateDir, "nanobots-recap", CreateAgentRequest{})
	if err != nil {
		t.Fatalf("EnsureAgent after remote deletion should self-heal, got: %v", err)
	}
	if id != "a2" || key != "ocv_a2" {
		t.Errorf("EnsureAgent = %q, %q, want the freshly created a2/ocv_a2", id, key)
	}
	if createCalls != 2 {
		t.Errorf("createCalls = %d, want 2 (the stale one replaced)", createCalls)
	}

	// And the new credential is what's now on disk, so the next process
	// doesn't repeat the whole dance.
	cred, ok, err := loadAgentCredential(stateDir, "nanobots-recap")
	if err != nil || !ok {
		t.Fatalf("loadAgentCredential = %v, %v", ok, err)
	}
	if cred.AgentID != "a2" {
		t.Errorf("saved credential is for %q, want the replacement a2", cred.AgentID)
	}
}

// An api_key is shown exactly once and can never be recovered, so a listing
// that's momentarily wrong must not cost you one. Nothing deletes the local
// credential — a failed replacement leaves it exactly where it was.
func TestEnsureAgentNeverDiscardsACredentialItCannotReplace(t *testing.T) {
	srv := newTestServer(t, map[string]http.HandlerFunc{
		"/v1/auth/api-key-token": tokenHandler(t),
		"/v1/agents": func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost {
				// What the 10-agent pro-tier cap looks like.
				w.WriteHeader(http.StatusForbidden)
				w.Write([]byte(`{"error":{"message":"agent limit reached"}}`))
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"agents": []Agent{}})
		},
	})
	c := NewClient("1ck_test")
	c.BaseURL = srv.URL
	stateDir := t.TempDir()
	if err := saveAgentCredential(stateDir, "nanobots-recap", agentCredential{AgentID: "gone", APIKey: "ocv_precious"}); err != nil {
		t.Fatal(err)
	}

	if _, _, err := c.EnsureAgent(stateDir, "nanobots-recap", CreateAgentRequest{}); err == nil {
		t.Fatal("expected an error when the replacement can't be created")
	}

	cred, ok, err := loadAgentCredential(stateDir, "nanobots-recap")
	if err != nil || !ok {
		t.Fatalf("the credential file was destroyed: ok=%v err=%v", ok, err)
	}
	if cred.APIKey != "ocv_precious" {
		t.Errorf("credential = %q, want it left exactly as it was", cred.APIKey)
	}
}

// A five-bot swarm calls EnsureAgent five times back to back; that must not
// be five listings of the same unchanged account.
func TestEnsureAgentDoesNotListOncePerBot(t *testing.T) {
	listCalls := 0
	existing := []Agent{{ID: "a1", Name: "nanobots-recap"}}
	srv := newTestServer(t, map[string]http.HandlerFunc{
		"/v1/auth/api-key-token": tokenHandler(t),
		"/v1/agents": func(w http.ResponseWriter, r *http.Request) {
			listCalls++
			json.NewEncoder(w).Encode(map[string]any{"agents": existing})
		},
	})
	c := NewClient("1ck_test")
	c.BaseURL = srv.URL
	stateDir := t.TempDir()
	if err := saveAgentCredential(stateDir, "nanobots-recap", agentCredential{AgentID: "a1", APIKey: "ocv_a1"}); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 5; i++ {
		if _, _, err := c.EnsureAgent(stateDir, "nanobots-recap", CreateAgentRequest{}); err != nil {
			t.Fatalf("EnsureAgent %d: %v", i, err)
		}
	}
	if listCalls != 1 {
		t.Errorf("listCalls = %d, want 1 — the listing should be cached across a swarm's bots", listCalls)
	}
}

// A locked vault and an unconnected account both surface as an error from
// GetSecret, but they need opposite advice — "unlock your passkey" vs.
// "connect the account". Callers used to report every one of these as the
// latter, which is actively misleading when the credential is sitting right
// there in a vault you just need to unlock.
func TestAsVaultLocked(t *testing.T) {
	lockedBody := `{"type":"about:blank","title":"Forbidden","status":403,` +
		`"detail":"Passkey verification required to access vault secrets. Unlock with your passkey."}`

	for _, tc := range []struct {
		name       string
		err        error
		want       bool
		wantDetail string
	}{
		{
			name:       "the real 403 1Claw returns for a locked vault",
			err:        &apiError{Status: 403, Body: []byte(lockedBody)},
			want:       true,
			wantDetail: "Passkey verification required to access vault secrets. Unlock with your passkey.",
		},
		{
			name: "a 403 that isn't about passkeys stays a plain 403",
			err:  &apiError{Status: 403, Body: []byte(`{"detail":"Agent limit reached (10/10 on pro tier)."}`)},
			want: false,
		},
		{
			name: "a 404 is a missing vault, not a locked one",
			err:  &apiError{Status: 404, Body: []byte(`{"detail":"vault not found"}`)},
			want: false,
		},
		{
			name: "an unrelated error is left alone",
			err:  errors.New("dial tcp: connection refused"),
			want: false,
		},
		{
			name: "it survives wrapping, since callers wrap before anyone checks",
			err:  fmt.Errorf("get slack token: %w", &apiError{Status: 403, Body: []byte(lockedBody)}),
			want: true,
			// The wrapper's own text must not swallow 1Claw's sentence.
			wantDetail: "Passkey verification required to access vault secrets. Unlock with your passkey.",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			locked, ok := AsVaultLocked(tc.err)
			if ok != tc.want {
				t.Fatalf("AsVaultLocked = %v, want %v", ok, tc.want)
			}
			if !ok {
				return
			}
			if locked.Detail != tc.wantDetail {
				t.Errorf("Detail = %q, want %q", locked.Detail, tc.wantDetail)
			}
			// Whatever else it says, a person reading it must learn the
			// vault is the problem.
			if !strings.Contains(strings.ToLower(locked.Error()), "vault is locked") {
				t.Errorf("Error() = %q, want it to name the locked vault", locked.Error())
			}
		})
	}
}

// The other 403 that reaches a user mid-run. Unlike the passkey one, 1Claw
// gives this a real machine-readable type, so match on that, not on prose.
func TestAsAgentLimit(t *testing.T) {
	capBody := `{"type":"resource_limit_exceeded","title":"Resource Limit Exceeded","status":403,` +
		`"detail":"Agent limit reached (10/10 on pro tier). Upgrade your plan for more."}`

	limit, ok := AsAgentLimit(&apiError{Status: 403, Body: []byte(capBody)})
	if !ok {
		t.Fatal("the real cap 403 was not recognised")
	}
	if !strings.Contains(limit.Detail, "10/10") {
		t.Errorf("Detail = %q, want 1Claw's own sentence including the numbers", limit.Detail)
	}

	// A locked vault is also a 403 and must not be mistaken for the cap —
	// the two need opposite advice.
	lockedBody := `{"type":"about:blank","status":403,"detail":"Passkey verification required."}`
	if _, ok := AsAgentLimit(&apiError{Status: 403, Body: []byte(lockedBody)}); ok {
		t.Error("a locked vault was classified as the agent cap")
	}
	if _, ok := AsVaultLocked(&apiError{Status: 403, Body: []byte(capBody)}); ok {
		t.Error("the agent cap was classified as a locked vault")
	}
	if _, ok := AsAgentLimit(errors.New("connection refused")); ok {
		t.Error("an unrelated error was classified as the agent cap")
	}
}

// The whole point is that the message names the bot and the fix, instead of
// pasting a JSON blob into a container error.
func TestEnsureAgentExplainsTheCap(t *testing.T) {
	srv := newTestServer(t, map[string]http.HandlerFunc{
		"/v1/auth/api-key-token": tokenHandler(t),
		"/v1/agents": func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost {
				w.WriteHeader(http.StatusForbidden)
				w.Write([]byte(`{"type":"resource_limit_exceeded","status":403,` +
					`"detail":"Agent limit reached (10/10 on pro tier). Upgrade your plan for more."}`))
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"agents": []Agent{}})
		},
	})
	c := NewClient("1ck_test")
	c.BaseURL = srv.URL

	_, _, err := c.EnsureAgent(t.TempDir(), "nanobots-content-ideas", CreateAgentRequest{})
	if err == nil {
		t.Fatal("expected an error at the cap")
	}
	msg := err.Error()
	for _, want := range []string{"nanobots-content-ideas", "10/10", "Free a slot"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q is missing %q", msg, want)
		}
	}
	if strings.Contains(msg, `{"type"`) {
		t.Errorf("error still contains a raw JSON body: %q", msg)
	}
}

// A Human key and an agent key are exchanged at different endpoints, and
// sending one to the other's endpoint fails with a bare 401 that says
// nothing useful. This is the seam that makes an agent able to open an
// approval at all, so it is worth pinning which path each client uses.
func TestAnAgentClientUsesTheAgentTokenEndpoint(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if strings.HasSuffix(r.URL.Path, "-token") {
			fmt.Fprint(w, `{"access_token":"t","expires_in":3600}`)
			return
		}
		fmt.Fprint(w, `{"vaults":[]}`)
	}))
	defer srv.Close()

	human := NewClient("1ck_human")
	human.BaseURL = srv.URL
	if _, err := human.ListVaults(); err != nil {
		t.Fatalf("human ListVaults: %v", err)
	}

	agent := NewAgentClient("ocv_agent")
	agent.BaseURL = srv.URL
	if _, err := agent.ListVaults(); err != nil {
		t.Fatalf("agent ListVaults: %v", err)
	}

	joined := strings.Join(paths, " ")
	if !strings.Contains(joined, "/v1/auth/api-key-token") {
		t.Errorf("the human client did not use the human exchange: %v", paths)
	}
	if !strings.Contains(joined, "/v1/auth/agent-token") {
		t.Errorf("the agent client did not use the agent exchange: %v", paths)
	}
}
