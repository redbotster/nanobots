package statedb

import (
	"path/filepath"
	"testing"
)

func TestOpenCreatesTheFileAndItsParentDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "nanobots.db")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec("CREATE TABLE t (id TEXT PRIMARY KEY)"); err != nil {
		t.Fatalf("CREATE TABLE: %v", err)
	}
}

func TestOpenEnablesWALMode(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "nanobots.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	var mode string
	if err := db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatalf("PRAGMA journal_mode: %v", err)
	}
	if mode != "wal" {
		t.Errorf("journal_mode = %q, want wal", mode)
	}
}

// The bug: an empty path doesn't fail, it opens SQLite's own private,
// anonymous temp database — Ping succeeds, everything looks fine, and
// whatever the caller thought it was reading or writing silently goes
// nowhere real. A missing path has to be a loud startup error instead.
func TestOpenRefusesAnEmptyPath(t *testing.T) {
	if _, err := Open(""); err == nil {
		t.Fatal("Open(\"\") succeeded — an empty path must be refused, not silently opened as an anonymous temp database")
	}
}

// A second Open against the same path (what happens every time nanobotd
// restarts) must reuse what's there, not fail or wipe it.
func TestOpenTwiceAgainstTheSameFileReusesData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nanobots.db")

	db1, err := Open(path)
	if err != nil {
		t.Fatalf("Open (first): %v", err)
	}
	if _, err := db1.Exec("CREATE TABLE t (id TEXT PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}
	if _, err := db1.Exec("INSERT INTO t (id) VALUES ('a')"); err != nil {
		t.Fatal(err)
	}
	db1.Close()

	db2, err := Open(path)
	if err != nil {
		t.Fatalf("Open (second): %v", err)
	}
	defer db2.Close()
	var id string
	if err := db2.QueryRow("SELECT id FROM t").Scan(&id); err != nil {
		t.Fatalf("SELECT: %v", err)
	}
	if id != "a" {
		t.Errorf("id = %q, want a", id)
	}
}
