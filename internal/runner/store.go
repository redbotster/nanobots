package runner

import (
	"database/sql"
	"fmt"
	"sync"

	"github.com/redbotster/nanobots/internal/statedb"
)

// MaxPersistedRuns bounds run history. Was 200, kept low deliberately
// because a directory of JSON files had no other way to stay bounded —
// raised to 1000 once SQLite made "how many rows are in this table" and
// "delete everything past the newest N" cheap regardless of the number.
// See docs/run-history.md for the measurement: loading 1,000 realistic
// runs from the old JSON format took ~1s at startup; from SQLite, querying
// the newest 1000 (of however many exist) is not meaningfully different
// from querying the newest 200 was.
const MaxPersistedRuns = 1000

// RunStore is nanobotd's registry of runs: every run of this process, plus
// whatever previous processes left behind.
//
// The in-memory map is the live view — every run this process has touched,
// running or finished, is here, because that's also where subscriptions,
// approvals and cancellation live and none of those have (or need) a disk
// representation. DB (when set) is the durable one: a run is written there
// once, on reaching a terminal state — see sqlpersist.go for why SQLite
// rather than the JSON-file-per-run this used to be.
type RunStore struct {
	mu   sync.RWMutex
	runs map[string]*Run

	// DB is where run history is persisted. nil means don't persist, which
	// is what every test that doesn't care gets by default.
	DB *sql.DB
}

func NewRunStore() *RunStore {
	return &RunStore{runs: map[string]*Run{}}
}

// NewPersistentRunStore returns a store backed by the SQLite file at
// dbPath, preloaded with the history already there. If dbPath has never
// been used before and jsonDir holds history in the old one-file-per-run
// format, it's imported once — see migrateJSONHistory. The JSON files are
// never deleted by this: a storage-format change doesn't get to remove
// data as a side effect, only add it in a new place. logf (nil is fine)
// hears about the migration and about the load — a load error is returned
// alongside a usable store either way; unreadable history is a reason to
// warn, never a reason to refuse to start.
func NewPersistentRunStore(dbPath, jsonDir string, logf func(string, ...any)) (*RunStore, error) {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	db, err := statedb.Open(dbPath)
	if err != nil {
		return &RunStore{runs: map[string]*Run{}}, err
	}
	if err := ensureRunsSchema(db); err != nil {
		return &RunStore{runs: map[string]*Run{}}, err
	}

	migrated, migrateErr := migrateJSONHistory(db, jsonDir)
	if migrateErr != nil {
		logf("migrating run history from %s to %s: %v", jsonDir, dbPath, migrateErr)
	} else if migrated > 0 {
		logf("migrated %d run(s) from %s to %s — the JSON files are untouched; "+
			"delete them yourself once you've confirmed history looks right", migrated, jsonDir, dbPath)
	}

	s := &RunStore{runs: map[string]*Run{}, DB: db}
	restored, loadErr := loadSnapshotsSQL(db)
	for i, r := range restored {
		if i >= MaxPersistedRuns {
			break
		}
		s.runs[r.ID] = r
	}
	if pruneErr := pruneSQL(db, MaxPersistedRuns); pruneErr != nil && loadErr == nil {
		loadErr = pruneErr
	}
	return s, loadErr
}

func (s *RunStore) Add(r *Run) {
	s.mu.Lock()
	s.runs[r.ID] = r
	db := s.DB
	s.mu.Unlock()

	if db == nil {
		return
	}
	// Persist when the run ends rather than now: a run in flight has an
	// empty log and no outputs, and writing it repeatedly as it progresses
	// would buy nothing that survives a crash anyway (the run itself
	// wouldn't). If the process dies mid-run there's simply no record —
	// stated plainly rather than papered over with a half-written row.
	r.onTerminal(func(done *Run) {
		if err := writeSnapshotSQL(db, done); err != nil {
			done.Log("", "", "could not save this run to history: %v", err)
		}
	})
}

func (s *RunStore) Get(id string) (*Run, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.runs[id]
	if !ok {
		return nil, fmt.Errorf("no run %s", id)
	}
	return r, nil
}

func (s *RunStore) List() []*Run {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Run, 0, len(s.runs))
	for _, r := range s.runs {
		out = append(out, r)
	}
	return out
}
