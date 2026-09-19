// Package statedb is the one SQLite file nanobotd's own state lives in:
// runs, run steps, and — as they migrate off their own separate formats in
// following changes — schedule state, circuit-breaker state, and key/value
// memory. One file, one connection pool, opened once at startup and shared
// by every subsystem that wants a table in it.
//
// There is deliberately no central migrations list. Each subsystem owns its
// own tables and creates them with `CREATE TABLE IF NOT EXISTS` the first
// time its store is constructed — the schema here is simple and additive
// enough (new tables, not restructured ones) that a shared version counter
// would add ceremony without buying anything yet. If that stops being true,
// this is the place to add one.
package statedb

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// Open returns a shared handle to the SQLite file at path, creating its
// parent directory and the file itself if neither exists.
//
// WAL mode lets readers (every /api/runs poll from every open tab) proceed
// without blocking on a writer (a run reaching a terminal state), which is
// the concurrency pattern this daemon actually has — and that only holds if
// database/sql is actually allowed to open more than one connection.
// MaxOpenConns(1) looked like the right reading of "single writer" at
// first, and was wrong: found live, not guessed, when a run store reload
// hung indefinitely with a real "one connection, one writer" pool, because
// the single connection was checked out by an outer query's still-open
// rows while a nested query tried to check out a second one that could
// never come free — a plain Go connection-pool deadlock, nothing to do
// with SQLite's own locking. "Single writer" is what SQLite's own file
// locking already guarantees (a second writer blocks or retries under
// busy_timeout, however many Go connections there are); it was never a
// reason to cap the pool at one.
//
// modernc.org/sqlite is pure Go, not cgo, for the same reason
// internal/secrets picked a pure-Go keychain library: this build cross-
// compiles for six platform/arch pairs from one machine (see
// docs/hosting.md), and a cgo dependency would need a C toolchain for each
// target instead of just `GOOS`/`GOARCH`.
func Open(path string) (*sql.DB, error) {
	// An empty path isn't "no file", it's "SQLite's own private, anonymous
	// temporary database" — found live, not guessed, when a test that
	// forgot to set Paths.DBPath still passed a Ping and returned an
	// empty, apparently-working store, silently discarding the run history
	// the test wrote and expected to read back. Refused here so a missing
	// path is a startup error, not a store that quietly answers "nothing
	// here" forever.
	if path == "" {
		return nil, fmt.Errorf("statedb: no path given (an empty string opens SQLite's own anonymous temp database, not this one)")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("statedb: create %s: %w", filepath.Dir(path), err)
	}
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, fmt.Errorf("statedb: open %s: %w", path, err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("statedb: open %s: %w", path, err)
	}
	return db, nil
}
