package api

import (
	"os"
	"path/filepath"
	"testing"
)

// The tab was called Fleet, and its state file fleet.json. Renaming the tab
// must not throw away every instruction someone had written: the whole
// point of recording what a bot shipped with is that a change can be
// undone, and losing the record loses that too.
func TestTeamStoreReadsTheOldFleetFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "fleet.json"),
		[]byte(`{"inbox-triage":{"shipped":"the original wording"}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	s := &TeamStore{Path: filepath.Join(dir, "team.json")}
	got, err := s.load()
	if err != nil {
		t.Fatal(err)
	}
	rec, ok := got["inbox-triage"]
	if !ok {
		t.Fatalf("the old file was ignored: %+v", got)
	}
	if rec.Shipped != "the original wording" {
		t.Errorf("shipped = %q", rec.Shipped)
	}
}

// Once there is a team.json it wins outright — an old fleet.json left on
// disk must not resurrect anything.
func TestTheNewFileWinsOverTheOld(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "fleet.json"), []byte(`{"old":{"shipped":"stale"}}`), 0o600)
	_ = os.WriteFile(filepath.Join(dir, "team.json"), []byte(`{"new":{"shipped":"current"}}`), 0o600)

	got, err := (&TeamStore{Path: filepath.Join(dir, "team.json")}).load()
	if err != nil {
		t.Fatal(err)
	}
	if _, stale := got["old"]; stale {
		t.Error("the superseded file was merged in")
	}
	if _, ok := got["new"]; !ok {
		t.Error("the current file was not read")
	}
}
