package runner

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// Run history moved off one-JSON-file-per-run and onto the shared SQLite
// file every other piece of nanobotd's own state lives in
// (internal/statedb) — see docs/run-history.md for the measurement that
// motivated it: loading 1,000 realistically-sized run histories from disk
// at startup took ~1 second, entirely up front, before the daemon could
// answer its first request. A run is still written exactly once, on
// reaching a terminal state, for the same reason as before — an in-flight
// run has an empty log and no outputs, and writing it repeatedly would buy
// nothing that survives a crash anyway.
//
// The table shapes mirror the old JSON snapshot deliberately: one `runs`
// row per run with the same fields snapshot used to carry, and one
// `run_log` row per LogEntry rather than a single JSON blob, so a run's log
// (unbounded, and the thing most likely to be large) doesn't have to be
// fully deserialized to answer "how many runs are there" or "what's this
// run's status" — the two things polled most often.

const runsSchema = `
CREATE TABLE IF NOT EXISTS runs (
	id               TEXT PRIMARY KEY,
	swarm_name       TEXT NOT NULL,
	swarm_path       TEXT NOT NULL DEFAULT '',
	status           TEXT NOT NULL,
	started_at       TEXT NOT NULL,
	finished_at      TEXT NOT NULL DEFAULT '',
	error            TEXT NOT NULL DEFAULT '',
	triggered_by     TEXT NOT NULL DEFAULT '',
	tolerated        TEXT NOT NULL DEFAULT '[]',
	captured         TEXT NOT NULL DEFAULT '{}',
	demo_services    TEXT NOT NULL DEFAULT '[]',
	stopped_by_user  INTEGER NOT NULL DEFAULT 0,
	declined_by_user INTEGER NOT NULL DEFAULT 0,
	nothing_to_do    TEXT NOT NULL DEFAULT '',
	outputs          TEXT NOT NULL DEFAULT '{}'
);
CREATE INDEX IF NOT EXISTS runs_started_at ON runs(started_at DESC);

CREATE TABLE IF NOT EXISTS run_log (
	run_id TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
	seq    INTEGER NOT NULL,
	time   TEXT NOT NULL,
	bot    TEXT NOT NULL DEFAULT '',
	step   TEXT NOT NULL DEFAULT '',
	msg    TEXT NOT NULL DEFAULT '',
	PRIMARY KEY (run_id, seq)
);
`

// ensureRunsSchema creates the runs/run_log tables if they don't already
// exist. Safe to call every time a store is opened — see internal/statedb's
// package doc for why there's no separate migrations list.
func ensureRunsSchema(db *sql.DB) error {
	_, err := db.Exec(runsSchema)
	return err
}

// writeSnapshotSQL persists one run's terminal state — an upsert, since a
// run is written exactly once but this must still be safe to call twice
// (e.g. a retried write after a transient disk error).
func writeSnapshotSQL(db *sql.DB, r *Run) error {
	s := snapshotOf(r)
	tolerated, err := json.Marshal(s.Tolerated)
	if err != nil {
		return err
	}
	captured, err := json.Marshal(s.Captured)
	if err != nil {
		return err
	}
	demoServices, err := json.Marshal(s.DemoServices)
	if err != nil {
		return err
	}
	outputs, err := json.Marshal(s.Outputs)
	if err != nil {
		return err
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`
		INSERT INTO runs (id, swarm_name, swarm_path, status, started_at, finished_at, error,
			triggered_by, tolerated, captured, demo_services, stopped_by_user, declined_by_user,
			nothing_to_do, outputs)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET
			swarm_name=excluded.swarm_name, swarm_path=excluded.swarm_path, status=excluded.status,
			started_at=excluded.started_at, finished_at=excluded.finished_at, error=excluded.error,
			triggered_by=excluded.triggered_by, tolerated=excluded.tolerated, captured=excluded.captured,
			demo_services=excluded.demo_services, stopped_by_user=excluded.stopped_by_user,
			declined_by_user=excluded.declined_by_user, nothing_to_do=excluded.nothing_to_do,
			outputs=excluded.outputs`,
		s.ID, s.SwarmName, s.SwarmPath, string(s.Status), timeString(s.StartedAt), timeString(s.FinishedAt),
		s.Error, s.TriggeredBy, string(tolerated), string(captured), string(demoServices),
		boolInt(s.StoppedByUser), boolInt(s.DeclinedByUser), s.NothingToDo, string(outputs),
	); err != nil {
		return fmt.Errorf("run %s: %w", s.ID, err)
	}

	if _, err := tx.Exec(`DELETE FROM run_log WHERE run_id = ?`, s.ID); err != nil {
		return err
	}
	for i, entry := range s.Log {
		if _, err := tx.Exec(`INSERT INTO run_log (run_id, seq, time, bot, step, msg) VALUES (?, ?, ?, ?, ?, ?)`,
			s.ID, i, timeString(entry.Time), entry.Bot, entry.Step, entry.Msg); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// loadSnapshotsSQL reads every run, newest first, log included. Mirrors
// loadSnapshots' JSON-era contract exactly: a run whose log can't be
// decoded is skipped rather than failing the whole load.
//
// Two queries, not one-per-run: the first version queried run_log inside
// the loop iterating the runs query's still-open rows, on the same *sql.DB
// — which deadlocked the moment there was more than one connection to
// juggle (see statedb.Open's doc comment for the exact failure). Loading
// every run's log in a single second pass sidesteps that entirely, and is
// the shape this should have been regardless: one round trip beats N.
func loadSnapshotsSQL(db *sql.DB) ([]*Run, error) {
	rows, err := db.Query(`
		SELECT id, swarm_name, swarm_path, status, started_at, finished_at, error, triggered_by,
			tolerated, captured, demo_services, stopped_by_user, declined_by_user, nothing_to_do, outputs
		FROM runs ORDER BY started_at DESC`)
	if err != nil {
		return nil, err
	}

	var snaps []snapshot
	for rows.Next() {
		var s snapshot
		var status, startedAt, finishedAt, tolerated, captured, demoServices, outputs string
		var stoppedByUser, declinedByUser int
		if err := rows.Scan(&s.ID, &s.SwarmName, &s.SwarmPath, &status, &startedAt, &finishedAt, &s.Error,
			&s.TriggeredBy, &tolerated, &captured, &demoServices, &stoppedByUser, &declinedByUser,
			&s.NothingToDo, &outputs); err != nil {
			rows.Close()
			return nil, err
		}
		s.Status = RunStatus(status)
		s.StoppedByUser = stoppedByUser != 0
		s.DeclinedByUser = declinedByUser != 0
		if s.StartedAt, err = parseTimeString(startedAt); err != nil {
			continue
		}
		s.FinishedAt, _ = parseTimeString(finishedAt) // zero value on "" is correct
		if err := json.Unmarshal([]byte(tolerated), &s.Tolerated); err != nil {
			continue
		}
		if err := json.Unmarshal([]byte(captured), &s.Captured); err != nil {
			continue
		}
		if err := json.Unmarshal([]byte(demoServices), &s.DemoServices); err != nil {
			continue
		}
		if err := json.Unmarshal([]byte(outputs), &s.Outputs); err != nil {
			continue
		}
		snaps = append(snaps, s)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}

	logs, err := loadAllRunLogs(db)
	if err != nil {
		return nil, err
	}

	out := make([]*Run, 0, len(snaps))
	for _, s := range snaps {
		s.Log = logs[s.ID]
		out = append(out, s.toRun())
	}
	return out, nil
}

// loadAllRunLogs reads every run_log row in one query and groups it by
// run_id, rather than one query per run.
func loadAllRunLogs(db *sql.DB) (map[string][]LogEntry, error) {
	rows, err := db.Query(`SELECT run_id, time, bot, step, msg FROM run_log ORDER BY run_id, seq`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	logs := map[string][]LogEntry{}
	for rows.Next() {
		var runID, t string
		var e LogEntry
		if err := rows.Scan(&runID, &t, &e.Bot, &e.Step, &e.Msg); err != nil {
			return nil, err
		}
		if e.Time, err = parseTimeString(t); err != nil {
			return nil, err
		}
		logs[runID] = append(logs[runID], e)
	}
	return logs, rows.Err()
}

// migrateJSONHistory imports every run from the old JSON-file-per-run
// format at jsonDir into db, but only the first time: if the runs table
// already has anything in it, this is a no-op, so a restart after a
// successful migration never re-imports (which would be harmless but
// wasteful) or double-counts. Returns how many runs were imported, so the
// caller can log it — 0 means either nothing needed migrating or jsonDir
// never existed, which on a fresh install is the same thing.
func migrateJSONHistory(db *sql.DB, jsonDir string) (int, error) {
	var existing int
	if err := db.QueryRow(`SELECT COUNT(*) FROM runs`).Scan(&existing); err != nil {
		return 0, err
	}
	if existing > 0 {
		return 0, nil
	}
	restored, err := loadSnapshots(jsonDir)
	if err != nil {
		return 0, err
	}
	for _, r := range restored {
		if err := writeSnapshotSQL(db, r); err != nil {
			return 0, fmt.Errorf("migrating run %s: %w", r.ID, err)
		}
	}
	return len(restored), nil
}

// pruneSQL deletes every run beyond the newest keep, same policy
// MaxPersistedRuns has always enforced — ON DELETE CASCADE takes its
// run_log rows with it.
func pruneSQL(db *sql.DB, keep int) error {
	_, err := db.Exec(`
		DELETE FROM runs WHERE id NOT IN (
			SELECT id FROM runs ORDER BY started_at DESC LIMIT ?
		)`, keep)
	return err
}

func timeString(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func parseTimeString(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	return time.Parse(time.RFC3339Nano, s)
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
