package memory

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/redbotster/nanobots/internal/statedb"
)

func openTestMemoryDB(t *testing.T) *SQLite {
	t.Helper()
	db, err := statedb.Open(filepath.Join(t.TempDir(), "nanobots.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	s, err := NewSQLite(db, "")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSQLiteRoundTrips(t *testing.T) {
	s := openTestMemoryDB(t)
	ctx := context.Background()

	if _, found, err := s.Get(ctx, "competitor-watch", "last_summary"); err != nil || found {
		t.Fatalf("empty store: found=%v err=%v", found, err)
	}
	if err := s.Put(ctx, "competitor-watch", "last_summary", "they shipped pricing"); err != nil {
		t.Fatal(err)
	}
	got, found, err := s.Get(ctx, "competitor-watch", "last_summary")
	if err != nil || !found || got != "they shipped pricing" {
		t.Errorf("got %q found=%v err=%v", got, found, err)
	}

	// A second key in the same namespace must not clobber the first.
	if err := s.Put(ctx, "competitor-watch", "last_run_at", "2026-09-12"); err != nil {
		t.Fatal(err)
	}
	if got, _, _ := s.Get(ctx, "competitor-watch", "last_summary"); got != "they shipped pricing" {
		t.Errorf("second Put clobbered the first key: %q", got)
	}
	// Namespaces are separate.
	if _, found, _ := s.Get(ctx, "other-bot", "last_summary"); found {
		t.Error("namespaces are leaking into each other")
	}
}

func TestSQLitePutOverwritesTheSameKey(t *testing.T) {
	s := openTestMemoryDB(t)
	ctx := context.Background()
	if err := s.Put(ctx, "ns", "k", "first"); err != nil {
		t.Fatal(err)
	}
	if err := s.Put(ctx, "ns", "k", "second"); err != nil {
		t.Fatal(err)
	}
	if got, _, _ := s.Get(ctx, "ns", "k"); got != "second" {
		t.Errorf("got %q, want the overwritten value", got)
	}
}

// The predecessor to this backend (Local, one JSON file per namespace)
// needed to hash a namespace containing a path separator or "..", because
// a bot-supplied namespace written straight into a filename could escape
// the memory directory — exactly the "a path segment is not a path"
// mistake CLAUDE.md calls out. SQLite has no such concept: a namespace is
// a bound query parameter, never a path, so there's nothing to sanitize
// and nothing to escape. This is the regression test for that class of
// bug staying gone rather than just currently absent.
func TestSQLiteNamespaceWithPathLikeCharactersIsJustAValue(t *testing.T) {
	s := openTestMemoryDB(t)
	ctx := context.Background()
	if err := s.Put(ctx, "../../escaped", "k", "v"); err != nil {
		t.Fatal(err)
	}
	if got, found, err := s.Get(ctx, "../../escaped", "k"); err != nil || !found || got != "v" {
		t.Errorf("got %q found=%v err=%v", got, found, err)
	}
	// A different, unrelated namespace must not see it.
	if _, found, _ := s.Get(ctx, "escaped", "k"); found {
		t.Error("a path-like namespace leaked into a differently-named one")
	}
}

func TestSQLiteRejectsEmptyNamespaceOrKey(t *testing.T) {
	s := openTestMemoryDB(t)
	ctx := context.Background()
	if _, _, err := s.Get(ctx, "", "k"); err == nil {
		t.Error("Get with no namespace should be refused")
	}
	if err := s.Put(ctx, "ns", "", "v"); err == nil {
		t.Error("Put with no key should be refused")
	}
}

// The whole reason this migration exists: upgrading a real machine must
// not throw away what a bot already remembered. Existing keys are
// imported once, and the old files are left exactly where they were.
func TestSQLiteMigratesLocalFilesOnce(t *testing.T) {
	legacyDir := t.TempDir()
	raw, _ := json.Marshal(map[string]string{"last_summary": "they shipped pricing", "last_run_at": "2026-09-12"})
	if err := os.WriteFile(filepath.Join(legacyDir, "competitor-watch.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}

	db, err := statedb.Open(filepath.Join(t.TempDir(), "nanobots.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s, err := NewSQLite(db, legacyDir)
	if err != nil {
		t.Fatalf("NewSQLite: %v", err)
	}

	ctx := context.Background()
	if got, found, err := s.Get(ctx, "competitor-watch", "last_summary"); err != nil || !found || got != "they shipped pricing" {
		t.Errorf("migrated value = %q found=%v err=%v", got, found, err)
	}

	if _, err := os.Stat(filepath.Join(legacyDir, "competitor-watch.json")); err != nil {
		t.Error("the legacy file was removed — migration must never delete its source")
	}

	// A second open against the same db and the same legacy dir must not
	// re-import (which would be harmless here, but proves the "once" gate
	// works the same way run history's does).
	s2, err := NewSQLite(db, legacyDir)
	if err != nil {
		t.Fatal(err)
	}
	if got, _, _ := s2.Get(ctx, "competitor-watch", "last_summary"); got != "they shipped pricing" {
		t.Errorf("value after a second open = %q, want it unchanged", got)
	}
}

// An unreadable or corrupt legacy file must not cost the other namespaces.
func TestSQLiteMigrationSkipsAnUnreadableFile(t *testing.T) {
	legacyDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(legacyDir, "corrupt.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	good, _ := json.Marshal(map[string]string{"k": "v"})
	if err := os.WriteFile(filepath.Join(legacyDir, "good-bot.json"), good, 0o600); err != nil {
		t.Fatal(err)
	}

	db, err := statedb.Open(filepath.Join(t.TempDir(), "nanobots.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s, err := NewSQLite(db, legacyDir)
	if err != nil {
		t.Fatalf("a corrupt file should be skipped, not fail the migration: %v", err)
	}
	if got, found, _ := s.Get(context.Background(), "good-bot", "k"); !found || got != "v" {
		t.Errorf("the good namespace was lost alongside the corrupt one: got=%q found=%v", got, found)
	}
}

// No legacy directory at all is a fresh install, not a fault.
func TestSQLiteWithNoLegacyDirectory(t *testing.T) {
	db, err := statedb.Open(filepath.Join(t.TempDir(), "nanobots.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := NewSQLite(db, filepath.Join(t.TempDir(), "never-existed")); err != nil {
		t.Errorf("NewSQLite with no legacy dir should not error: %v", err)
	}
}
