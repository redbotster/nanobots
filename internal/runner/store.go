package runner

import (
	"fmt"
	"sync"
)

// RunStore is nanobotd's in-memory registry of runs. Per the MVP scope, run
// history doesn't survive a restart — see NANOBOTS-BLUEPRINT.md's roadmap
// for where a persistent run store fits later.
type RunStore struct {
	mu   sync.RWMutex
	runs map[string]*Run
}

func NewRunStore() *RunStore {
	return &RunStore{runs: map[string]*Run{}}
}

func (s *RunStore) Add(r *Run) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runs[r.ID] = r
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
