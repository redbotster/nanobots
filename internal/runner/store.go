package runner

import (
	"database/sql"
	"fmt"
	"sort"
	"sync"
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

	changedMu sync.Mutex
	changed   map[chan string]bool
}

func NewRunStore() *RunStore {
	return &RunStore{runs: map[string]*Run{}}
}

// NewPersistentRunStore returns a store backed by db (already open — see
// internal/statedb.Open; the caller owns it because it's shared with other
// subsystems now, not runner's alone), preloaded with the history already
// there. If db's runs table has never been used and jsonDir holds history
// in the old one-file-per-run format, it's imported once — see
// migrateJSONHistory. The JSON files are never deleted by this: a
// storage-format change doesn't get to remove data as a side effect, only
// add it in a new place. logf (nil is fine) hears about the migration and
// about the load — a load error is returned alongside a usable store
// either way; unreadable history is a reason to warn, never a reason to
// refuse to start.
func NewPersistentRunStore(db *sql.DB, jsonDir string, logf func(string, ...any)) (*RunStore, error) {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	if err := ensureRunsSchema(db); err != nil {
		return &RunStore{runs: map[string]*Run{}}, err
	}

	migrated, migrateErr := migrateJSONHistory(db, jsonDir)
	if migrateErr != nil {
		logf("migrating run history from %s: %v", jsonDir, migrateErr)
	} else if migrated > 0 {
		logf("migrated %d run(s) from %s into the shared database — the JSON files are untouched; "+
			"delete them yourself once you've confirmed history looks right", migrated, jsonDir)
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

	// A brand new run is itself a change worth telling subscribers about —
	// the runs-list SSE stream needs to learn about it the moment it
	// exists, not wait for its first status change. Then every later
	// mutation the summary cares about (SetStatus, SetError,
	// SetNothingToDo, Stop, Decide — see notifyChanged's call sites)
	// broadcasts the same way.
	r.onChange(func(changed *Run) { s.broadcastChanged(changed.ID) })
	s.broadcastChanged(r.ID)

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
		s.evictOldestBeyondCap()
	})
}

// evictOldestBeyondCap drops the oldest *finished* runs from the in-memory
// map once it holds more than MaxPersistedRuns — the same cap the DB layer
// already enforces on the persisted table (pruneSQL), now applied to the
// live map too.
//
// Before this, the map only ever grew: a swarm on a 30-minute schedule is
// ~17,500 runs a year, each holding its full log and every bot's outputs,
// and nothing ever freed one for as long as nanobotd kept running. Only
// terminal runs are ever candidates — a run still in progress owns real
// state (subscribers, pending approvals) that eviction would corrupt, and
// nothing prunes work that isn't done yet.
//
// A run dropped here is not gone: Get falls back to the database for
// anything the map no longer holds, so evicting from memory only means the
// oldest, coldest runs stop paying rent in RAM — it never makes a run in
// history unreachable.
func (s *RunStore) evictOldestBeyondCap() {
	s.mu.Lock()
	defer s.mu.Unlock()
	over := len(s.runs) - MaxPersistedRuns
	if over <= 0 {
		return
	}
	terminal := make([]*Run, 0, len(s.runs))
	for _, r := range s.runs {
		if r.GetStatus() == StatusSucceeded || r.GetStatus() == StatusFailed {
			terminal = append(terminal, r)
		}
	}
	if over > len(terminal) {
		over = len(terminal)
	}
	sort.Slice(terminal, func(i, j int) bool { return terminal[i].StartedAt.Before(terminal[j].StartedAt) })
	for _, r := range terminal[:over] {
		delete(s.runs, r.ID)
	}
}

// Get returns a run by id — from the live map if this process still holds
// it, or reconstructed from the database if it has aged out of memory (see
// evictOldestBeyondCap). The reconstructed copy is never cached back into
// the map: it is read-only history, and caching it would just undo the
// eviction it came from.
func (s *RunStore) Get(id string) (*Run, error) {
	s.mu.RLock()
	r, ok := s.runs[id]
	db := s.DB
	s.mu.RUnlock()
	if ok {
		return r, nil
	}
	if db != nil {
		if r, err := loadOneSnapshotSQL(db, id); err == nil && r != nil {
			return r, nil
		}
	}
	return nil, fmt.Errorf("no run %s", id)
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

// SubscribeChanges returns a channel of run IDs, one value each time that
// run's list-relevant state changes (added, or any of the fields
// runSummaryToJSON carries). For the runs-list SSE stream
// (internal/api.handleRunsEvents), which replaces polling GET /api/runs —
// see docs/runs.md.
func (s *RunStore) SubscribeChanges() chan string {
	ch := make(chan string, 64)
	s.changedMu.Lock()
	if s.changed == nil {
		s.changed = map[chan string]bool{}
	}
	s.changed[ch] = true
	s.changedMu.Unlock()
	return ch
}

func (s *RunStore) UnsubscribeChanges(ch chan string) {
	s.changedMu.Lock()
	defer s.changedMu.Unlock()
	if _, ok := s.changed[ch]; !ok {
		return
	}
	delete(s.changed, ch)
	close(ch)
}

func (s *RunStore) broadcastChanged(runID string) {
	s.changedMu.Lock()
	defer s.changedMu.Unlock()
	for ch := range s.changed {
		select {
		case ch <- runID:
		default: // a slow subscriber never blocks a run's own goroutine
		}
	}
}
