package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/redbotster/nanobots/internal/roles"
)

func serverWithRoles(t *testing.T) *Server {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(file), "..", "..")
	srv := testServer(t)
	srv.Roles = &roles.Store{
		CatalogPath:  filepath.Join(root, "roles", "roles.yaml"),
		OverridePath: filepath.Join(t.TempDir(), "roles.json"),
	}
	return srv
}

func callRoles(t *testing.T, srv *Server, method, path, body string) rolesResponse {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, bytes.NewReader([]byte(body)))
	}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s %s -> %d: %s", method, path, rec.Code, rec.Body.String())
	}
	var out rolesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// The library and the roster are both returned, because "what will the
// model actually see" is the question someone editing a role is trying to
// answer, and a UI reconstructing it from the list would drift.
func TestListingRolesReturnsTheLibraryAndTheRealRoster(t *testing.T) {
	got := callRoles(t, serverWithRoles(t), http.MethodGet, "/api/roles", "")
	if len(got.Roles) < 5 {
		t.Fatalf("got %d roles", len(got.Roles))
	}
	if got.Error != "" {
		t.Errorf("error on a clean install: %s", got.Error)
	}
	for _, r := range got.Roles {
		if !strings.Contains(got.Roster, r.Name) {
			t.Errorf("%q is in the library but not the roster", r.Name)
		}
	}
}

// Edit, see it in the roster, put it back — the loop the Fleet page is for.
func TestEditingARoleChangesTheRosterAndCanBeUndone(t *testing.T) {
	srv := serverWithRoles(t)
	before := callRoles(t, srv, http.MethodGet, "/api/roles", "")

	got := callRoles(t, srv, http.MethodPost, "/api/roles/security",
		`{"name":"Security engineer","focus":"Only whether a secret can reach a run log."}`)
	if !strings.Contains(got.Roster, "run log") {
		t.Errorf("roster did not change:\n%s", got.Roster)
	}
	var edited roles.Role
	for _, r := range got.Roles {
		if r.ID == "security" {
			edited = r
		}
	}
	if edited.Shipped == "" {
		t.Error("no shipped value recorded, so the UI cannot offer to put it back")
	}

	got = callRoles(t, srv, http.MethodPost, "/api/roles/security/reset", "")
	if got.Roster != before.Roster {
		t.Errorf("reset did not restore the roster")
	}
}

// A role you add is yours: it appears in the roster, and resetting removes
// it rather than restoring an empty shipped value.
func TestAddingYourOwnRole(t *testing.T) {
	srv := serverWithRoles(t)
	got := callRoles(t, srv, http.MethodPost, "/api/roles/legal",
		`{"name":"Legal","focus":"Whether anything here promises something the company cannot keep."}`)
	if !strings.Contains(got.Roster, "Legal") {
		t.Errorf("added role missing from the roster:\n%s", got.Roster)
	}
	got = callRoles(t, srv, http.MethodPost, "/api/roles/legal/reset", "")
	if strings.Contains(got.Roster, "Legal") {
		t.Error("reset did not remove an added role")
	}
}

// Retiring takes a role out of the roster but leaves it in the library —
// hiding it entirely would leave no way to switch it back on.
func TestRetiringARoleKeepsItInTheLibrary(t *testing.T) {
	srv := serverWithRoles(t)
	got := callRoles(t, srv, http.MethodPost, "/api/roles/qa", `{"retired":true}`)
	if strings.Contains(got.Roster, "QA") {
		t.Error("a retired role is still offered to the board")
	}
	var found bool
	for _, r := range got.Roles {
		if r.ID == "qa" {
			found = true
			if !r.Retired {
				t.Error("not marked retired")
			}
		}
	}
	if !found {
		t.Error("a retired role vanished from the library")
	}
}

// Bad input is the user's to fix, so it comes back as a 400 with the
// reason — not a 500, and not a silently-stored empty role that would
// later render as a blank line in a prompt.
func TestABadRoleIsRejectedWithTheReason(t *testing.T) {
	srv := serverWithRoles(t)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/roles/x",
		bytes.NewReader([]byte(`{"name":"X","focus":"  "}`))))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "focus") {
		t.Errorf("body does not say what is wrong: %s", rec.Body.String())
	}
}

// A daemon with no library configured must report an empty one rather than
// failing — review-board then invents a team, exactly as it did before.
func TestNoLibraryIsAnEmptyListNotAnError(t *testing.T) {
	srv := testServer(t) // no Roles
	got := callRoles(t, srv, http.MethodGet, "/api/roles", "")
	if len(got.Roles) != 0 || got.Roster != "" {
		t.Errorf("got %+v", got)
	}
}
