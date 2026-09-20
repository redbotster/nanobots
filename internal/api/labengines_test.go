package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/redbotster/nanobots/internal/secrets"
	"github.com/redbotster/nanobots/internal/team"
)

// engineTestServer wires up a Server with a real (temp-dir) Preferences
// and secrets store — no fakes needed, since both are already just local
// files.
func engineTestServer(t *testing.T, claudeConfigured, geminiConfigured bool) *Server {
	t.Helper()
	srv := testServer(t)
	prefs, err := team.NewPreferences(filepath.Join(t.TempDir(), "team-engines.json"), "")
	if err != nil {
		t.Fatal(err)
	}
	srv.LabEngines = prefs
	srv.LabTeamDir = t.TempDir()
	srv.LabClaudeConfigured = claudeConfigured
	srv.LabGeminiConfigured = geminiConfigured
	srv.Secrets = &secrets.File{Dir: t.TempDir()}
	return srv
}

// mkRole gives a role a workspace directory, the only thing that makes
// internal/team.Roles list it — there's no other catalog.
func mkRole(t *testing.T, teamDir, role string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(teamDir, role), 0o700); err != nil {
		t.Fatal(err)
	}
}

func doJSON(t *testing.T, srv *Server, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(method, path, reader))
	return rec
}

func TestLabEnginesStatusListsEveryRoleThatHasEverBeenDelegatedTo(t *testing.T) {
	srv := engineTestServer(t, true, true)
	mkRole(t, srv.LabTeamDir, "designer")
	mkRole(t, srv.LabTeamDir, "backend-engineer")
	if err := srv.LabEngines.SetDefault(team.EngineClaude); err != nil {
		t.Fatal(err)
	}
	if err := srv.LabEngines.SetRoleEngine("designer", team.EngineGemini); err != nil {
		t.Fatal(err)
	}

	rec := doJSON(t, srv, http.MethodGet, "/api/lab/engines", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
	}
	var resp labEnginesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.DefaultEngine != team.EngineClaude || !resp.ClaudeConfigured || !resp.GeminiConfigured {
		t.Errorf("resp = %+v", resp)
	}
	if len(resp.Roles) != 2 {
		t.Fatalf("roles = %+v, want designer and backend-engineer", resp.Roles)
	}
	byRole := map[string]labEngineRole{}
	for _, r := range resp.Roles {
		byRole[r.Role] = r
	}
	if byRole["designer"].Engine != team.EngineGemini || !byRole["designer"].Overridden {
		t.Errorf("designer = %+v, want gemini, overridden", byRole["designer"])
	}
	if byRole["backend-engineer"].Engine != team.EngineClaude || byRole["backend-engineer"].Overridden {
		t.Errorf("backend-engineer = %+v, want claude, not overridden (falls back to default)", byRole["backend-engineer"])
	}
}

func TestSetDefaultLabEngineTakesEffectImmediately(t *testing.T) {
	srv := engineTestServer(t, true, true)
	rec := doJSON(t, srv, http.MethodPost, "/api/lab/engines/default", setEngineRequest{Engine: team.EngineGemini})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
	}
	// The same pointer lab.Config.Engines would hold — no restart, no
	// re-read, this is the live value the very next delegation sees.
	if srv.LabEngines.Default() != team.EngineGemini {
		t.Errorf("Default() = %q, want gemini", srv.LabEngines.Default())
	}
}

func TestSetDefaultLabEngineRefusesAnEngineWithNoCredential(t *testing.T) {
	srv := engineTestServer(t, true, false) // no Gemini key
	rec := doJSON(t, srv, http.MethodPost, "/api/lab/engines/default", setEngineRequest{Engine: team.EngineGemini})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body: %s", rec.Code, rec.Body.String())
	}
	if srv.LabEngines.Default() == team.EngineGemini {
		t.Error("a refused engine must not become the live default")
	}
}

func TestSetRoleLabEngineOverridesJustThatRole(t *testing.T) {
	srv := engineTestServer(t, true, true)
	if err := srv.LabEngines.SetDefault(team.EngineClaude); err != nil {
		t.Fatal(err)
	}
	rec := doJSON(t, srv, http.MethodPost, "/api/lab/engines/roles/designer", setEngineRequest{Engine: team.EngineGemini})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
	}
	if srv.LabEngines.EngineFor("designer") != team.EngineGemini {
		t.Errorf("designer's engine = %q, want gemini", srv.LabEngines.EngineFor("designer"))
	}
	if srv.LabEngines.EngineFor("backend-engineer") != team.EngineClaude {
		t.Errorf("an unrelated role's engine changed: %q", srv.LabEngines.EngineFor("backend-engineer"))
	}
}

func TestSetRoleLabEngineEmptyClearsTheOverride(t *testing.T) {
	srv := engineTestServer(t, true, true)
	if err := srv.LabEngines.SetDefault(team.EngineClaude); err != nil {
		t.Fatal(err)
	}
	if err := srv.LabEngines.SetRoleEngine("designer", team.EngineGemini); err != nil {
		t.Fatal(err)
	}
	rec := doJSON(t, srv, http.MethodPost, "/api/lab/engines/roles/designer", setEngineRequest{Engine: ""})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
	}
	if srv.LabEngines.EngineFor("designer") != team.EngineClaude {
		t.Errorf("designer's engine after clearing = %q, want the default %q", srv.LabEngines.EngineFor("designer"), team.EngineClaude)
	}
}

func TestSetLabEngineKeyStoresItAndSaysARestartIsNeeded(t *testing.T) {
	srv := engineTestServer(t, false, false)
	rec := doJSON(t, srv, http.MethodPost, "/api/lab/engines/keys/gemini", setEngineKeyRequest{Token: "AIzaSy-test"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("restart")) {
		t.Errorf("body = %s, want it to say a restart is needed", rec.Body.String())
	}
	v, found, err := srv.Secrets.Get("gemini/api_key")
	if err != nil || !found || v != "AIzaSy-test" {
		t.Errorf("Secrets.Get(gemini/api_key) = %q, %v, %v", v, found, err)
	}
}

func TestSetLabEngineKeyRejectsAnUnknownEngine(t *testing.T) {
	srv := engineTestServer(t, false, false)
	rec := doJSON(t, srv, http.MethodPost, "/api/lab/engines/keys/chatgpt", setEngineKeyRequest{Token: "x"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body: %s", rec.Code, rec.Body.String())
	}
}

func TestSetLabEngineKeyRejectsAnEmptyToken(t *testing.T) {
	srv := engineTestServer(t, false, false)
	rec := doJSON(t, srv, http.MethodPost, "/api/lab/engines/keys/anthropic", setEngineKeyRequest{Token: "  "})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body: %s", rec.Code, rec.Body.String())
	}
}

func TestLabEnginesEndpointsAreQuietWhenLabIsntConfigured(t *testing.T) {
	srv := testServer(t) // no LabEngines set at all
	rec := doJSON(t, srv, http.MethodGet, "/api/lab/engines", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
	}
	var resp labEnginesResponse
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if len(resp.Roles) != 0 {
		t.Errorf("roles = %+v, want none", resp.Roles)
	}

	rec = doJSON(t, srv, http.MethodPost, "/api/lab/engines/default", setEngineRequest{Engine: team.EngineClaude})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 rather than a nil-pointer panic", rec.Code)
	}
}
