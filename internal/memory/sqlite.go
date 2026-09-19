package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SQLite is namespaced key/value memory backed by the same shared database
// run history and scheduler state live in (internal/statedb) — replacing
// Local, one JSON file per namespace under ~/.nanobots/memory/.
type SQLite struct {
	DB *sql.DB
}

const kvMemorySchema = `
CREATE TABLE IF NOT EXISTS kv_memory (
	namespace TEXT NOT NULL,
	key       TEXT NOT NULL,
	value     TEXT NOT NULL,
	PRIMARY KEY (namespace, key)
);
`

// NewSQLite opens the kv_memory table and, the first time it's empty,
// imports whatever Local's old namespace files (under legacyDir — "" skips
// this entirely) hold — once, the same way run history's JSON-to-SQLite
// migration works: read every *.json file, insert its keys, and leave the
// files on disk untouched. A storage-format change doesn't get to lose
// data as a side effect of moving it, and this runs at startup, before any
// bot exists to write to a namespace concurrently, so there's no moving
// target to race.
func NewSQLite(db *sql.DB, legacyDir string) (*SQLite, error) {
	if _, err := db.Exec(kvMemorySchema); err != nil {
		return nil, fmt.Errorf("memory: create kv_memory table: %w", err)
	}
	s := &SQLite{DB: db}
	if legacyDir != "" {
		if _, err := s.migrateLocalFiles(legacyDir); err != nil {
			return s, fmt.Errorf("memory: migrating local files from %s: %w", legacyDir, err)
		}
	}
	return s, nil
}

// migrateLocalFiles imports every namespace *.json file in dir, but only
// if kv_memory is currently empty — so a restart after a successful
// migration never re-imports or double-counts. Returns how many
// namespace/key pairs were imported.
func (s *SQLite) migrateLocalFiles(dir string) (int, error) {
	var existing int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM kv_memory`).Scan(&existing); err != nil {
		return 0, err
	}
	if existing > 0 {
		return 0, nil
	}
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}

	imported := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue // unreadable file: skip it, not the rest
		}
		var kv map[string]string
		if err := json.Unmarshal(raw, &kv); err != nil {
			continue // one bad file must not cost the rest
		}
		// internal/step always calls with the bot's own name as the
		// namespace (nb.Metadata.Name — see interpret.go), always a plain
		// hyphenated identifier, so it's always stored under its own
		// readable filename by Local.namespaceFile, never hashed. A
		// namespace that did need hashing would be unrecoverable here —
		// its real name isn't in the hash — but nothing in this codebase
		// ever produces one.
		namespace := strings.TrimSuffix(e.Name(), ".json")
		for k, v := range kv {
			if _, err := s.DB.Exec(`INSERT INTO kv_memory (namespace, key, value) VALUES (?, ?, ?)`, namespace, k, v); err != nil {
				return imported, err
			}
			imported++
		}
	}
	return imported, nil
}

func (s *SQLite) Get(ctx context.Context, namespace, key string) (string, bool, error) {
	if err := validKey(namespace, key); err != nil {
		return "", false, err
	}
	var value string
	err := s.DB.QueryRowContext(ctx, `SELECT value FROM kv_memory WHERE namespace = ? AND key = ?`, namespace, key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return value, true, nil
}

func (s *SQLite) Put(ctx context.Context, namespace, key, value string) error {
	if err := validKey(namespace, key); err != nil {
		return err
	}
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO kv_memory (namespace, key, value) VALUES (?, ?, ?)
		ON CONFLICT (namespace, key) DO UPDATE SET value = excluded.value`,
		namespace, key, value)
	return err
}

var _ Store = (*SQLite)(nil)
