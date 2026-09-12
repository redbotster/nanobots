package roles

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// storeOn builds a Store over the repo's real catalog and a temp override
// file, so the tests exercise the roster people actually ship with.
func storeOn(t *testing.T) *Store {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(file), "..", "..")
	return &Store{
		CatalogPath:  filepath.Join(root, "roles", "roles.yaml"),
		OverridePath: filepath.Join(t.TempDir(), "roles.json"),
	}
}

// The shipped catalog is what a fresh clone reviews with, so it has to load
// and be usable without anyone configuring anything.
func TestTheShippedCatalogLoads(t *testing.T) {
	got, err := storeOn(t).List()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) < 5 {
		t.Fatalf("only %d roles shipped — a board picking 2-5 from this has no room to choose", len(got))
	}
	ids := map[string]bool{}
	for _, r := range got {
		if r.ID == "" || r.Name == "" || strings.TrimSpace(r.Focus) == "" {
			t.Errorf("incomplete role: %+v", r)
		}
		if ids[r.ID] {
			t.Errorf("duplicate role id %q", r.ID)
		}
		ids[r.ID] = true
		if r.Shipped != "" || r.Custom || r.Retired {
			t.Errorf("a clean install should have no edits, got %+v", r)
		}
	}
}

// The roster is what a model reads, so it has to be one line per role with
// the name first — the board is told to echo the name back verbatim.
func TestTheRosterIsOneSharpLinePerRole(t *testing.T) {
	roster, err := storeOn(t).Roster()
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(roster, "\n")
	if len(lines) < 5 {
		t.Fatalf("roster has %d lines", len(lines))
	}
	for _, l := range lines {
		if !strings.HasPrefix(l, "- ") || !strings.Contains(l, " — ") {
			t.Errorf("line is not %q: %q", "- Name — focus", l)
		}
		// A folded YAML block would otherwise arrive as several lines and
		// the roster would read as twice as many roles.
		if strings.Contains(l, "\n") {
			t.Errorf("line wrapped: %q", l)
		}
	}
}

// An edit has to survive, show what it replaced, and be undoable — the
// whole point of keeping overrides separate from the repo file.
func TestAnEditIsKeptAndCanBePutBack(t *testing.T) {
	s := storeOn(t)
	before, _ := s.List()
	var original string
	for _, r := range before {
		if r.ID == "security" {
			original = r.Focus
		}
	}
	if original == "" {
		t.Fatal("no shipped security role to edit")
	}

	if err := s.Set("security", "Security engineer", "Only whether secrets can leak into a run log."); err != nil {
		t.Fatal(err)
	}
	after, _ := s.List()
	var edited Role
	for _, r := range after {
		if r.ID == "security" {
			edited = r
		}
	}
	if !strings.Contains(edited.Focus, "run log") {
		t.Errorf("edit did not stick: %q", edited.Focus)
	}
	if edited.Shipped != original {
		t.Errorf("shipped value not recorded: %q", edited.Shipped)
	}
	if edited.Custom {
		t.Error("an edited shipped role is not a custom one")
	}

	if err := s.Reset("security"); err != nil {
		t.Fatal(err)
	}
	after, _ = s.List()
	for _, r := range after {
		if r.ID == "security" && r.Focus != original {
			t.Errorf("reset did not restore: %q", r.Focus)
		}
	}

	// Editing must never touch the repo's own catalog — a `git pull`
	// should not fight a user's changes, and `git status` should stay
	// clean after using the app.
	raw, err := os.ReadFile(s.CatalogPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "run log") {
		t.Error("an edit was written into the repo's roles.yaml")
	}
}

// A role the user wrote has nothing to go back to, so Reset removes it
// rather than restoring an empty shipped value.
func TestAddingAndRemovingYourOwnRole(t *testing.T) {
	s := storeOn(t)
	if err := s.Set("legal", "Legal", "Whether anything here makes a promise the company cannot keep."); err != nil {
		t.Fatal(err)
	}
	roster, _ := s.Roster()
	if !strings.Contains(roster, "Legal — Whether anything") {
		t.Errorf("added role missing from the roster:\n%s", roster)
	}
	all, _ := s.List()
	for _, r := range all {
		if r.ID == "legal" && !r.Custom {
			t.Error("an added role should be marked custom")
		}
	}

	if err := s.Reset("legal"); err != nil {
		t.Fatal(err)
	}
	roster, _ = s.Roster()
	if strings.Contains(roster, "Legal") {
		t.Error("reset did not remove an added role")
	}
}

// Retiring keeps the wording so it can be switched back on — and an
// upgrade that changes a shipped role's focus must not quietly resurrect
// one the user switched off.
func TestARetiredRoleLeavesTheRosterButNotTheLibrary(t *testing.T) {
	s := storeOn(t)
	if err := s.SetRetired("qa", true); err != nil {
		t.Fatal(err)
	}
	roster, _ := s.Roster()
	if strings.Contains(roster, "QA") {
		t.Error("a retired role is still offered to the board")
	}
	all, _ := s.List()
	var found bool
	for _, r := range all {
		if r.ID == "qa" {
			found, _ = true, r
			if !r.Retired {
				t.Error("not marked retired")
			}
		}
	}
	if !found {
		t.Error("a retired role vanished from the library, so there is no way to switch it back on")
	}

	if err := s.SetRetired("qa", false); err != nil {
		t.Fatal(err)
	}
	roster, _ = s.Roster()
	if !strings.Contains(roster, "QA") {
		t.Error("could not switch a role back on")
	}
}

// The focus is one line in a prompt. Rejecting the obvious mistakes here
// beats discovering them when a board's prompt is four pages long.
func TestARoleIsValidatedBeforeItReachesAPrompt(t *testing.T) {
	s := storeOn(t)
	for _, tc := range []struct{ name, id, role, focus string }{
		{"empty id", "", "X", "focus"},
		{"spaces in id", "staff engineer", "X", "focus"},
		{"upper case id", "Security", "X", "focus"},
		{"no name", "x", "  ", "focus"},
		{"no focus", "x", "X", "   "},
		{"essay for a focus", "x", "X", strings.Repeat("a", maxFocus+1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := s.Set(tc.id, tc.role, tc.focus); err == nil {
				t.Error("accepted")
			}
		})
	}

	// A multi-line focus is collapsed rather than rejected: pasting a
	// wrapped sentence is a reasonable thing to do, and the roster is
	// line-oriented.
	if err := s.Set("x", "X", "first line\n  second line"); err != nil {
		t.Fatal(err)
	}
	roster, _ := s.Roster()
	if !strings.Contains(roster, "X — first line second line") {
		t.Errorf("multi-line focus not collapsed:\n%s", roster)
	}
}

// No catalog and no overrides is a legitimate state — a deployment that
// never set one up — and must produce an empty roster rather than an
// error, so review-board falls back to inventing roles as it always did.
func TestNoCatalogIsAnEmptyRosterNotAFailure(t *testing.T) {
	s := &Store{OverridePath: filepath.Join(t.TempDir(), "roles.json")}
	roster, err := s.Roster()
	if err != nil {
		t.Fatalf("an unconfigured install should not fail: %v", err)
	}
	if roster != "" {
		t.Errorf("roster = %q, want empty", roster)
	}
}
