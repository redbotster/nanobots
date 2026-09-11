package runner

import (
	"fmt"
	"sync"
)

// MaxPersistedRuns bounds the history directory. High enough that nobody
// hits it in normal use, low enough that the directory can't grow forever.
const MaxPersistedRuns = 200

// RunStore is nanobotd's registry of runs: every run of this process, plus
// whatever previous processes left behind.
//
// The in-memory map is the live view; Dir (when set) is the durable one.
// Runs are written to disk once, on reaching a terminal state — see
// persist.go for why a directory of JSON files rather than a database.
type RunStore struct {
	mu   sync.RWMutex
	runs map[string]*Run

	// Dir is where run history is persisted. Empty means don't persist,
	// which is what every test that doesn't care gets by default.
	Dir string
}

func NewRunStore() *RunStore {
	return &RunStore{runs: map[string]*Run{}}
}

// NewPersistentRunStore returns a store backed by dir, preloaded with the
// history already there. A load error is returned alongside a usable store:
// unreadable history is a reason to warn, never a reason to refuse to start.
func NewPersistentRunStore(dir string) (*RunStore, error) {
	s := &RunStore{runs: map[string]*Run{}, Dir: dir}
	restored, err := loadSnapshots(dir)
	for i, r := range restored {
		if i >= MaxPersistedRuns {
			break
		}
		s.runs[r.ID] = r
	}
	prune(dir, restored, MaxPersistedRuns)
	return s, err
}

func (s *RunStore) Add(r *Run) {
	s.mu.Lock()
	s.runs[r.ID] = r
	dir := s.Dir
	s.mu.Unlock()

	if dir == "" {
		return
	}
	// Persist when the run ends rather than now: a run in flight has an
	// empty log and no outputs, and writing it repeatedly as it progresses
	// would buy nothing that survives a crash anyway (the run itself
	// wouldn't). If the process dies mid-run there's simply no record —
	// stated plainly rather than papered over with a half-written file.
	r.onTerminal(func(done *Run) {
		if err := writeSnapshot(dir, done); err != nil {
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
