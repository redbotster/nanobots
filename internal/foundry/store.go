package foundry

import (
	"fmt"
	"sync"
)

// JobStore is a deliberate near-duplicate of runner.RunStore rather than a
// reuse of it: RunStore is typed to *runner.Run, not an interface, so it
// can't hold a *Job without generics (used nowhere else in this codebase)
// or an adapter — a ~20-line parallel store is less invasive than either,
// and keeps internal/runner untouched. Purely in-memory, same as
// RunStore — job history doesn't survive a restart.
type JobStore struct {
	mu   sync.RWMutex
	jobs map[string]*Job
}

func NewJobStore() *JobStore {
	return &JobStore{jobs: map[string]*Job{}}
}

func (s *JobStore) Add(j *Job) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.jobs[j.ID] = j
}

func (s *JobStore) Get(id string) (*Job, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	j, ok := s.jobs[id]
	if !ok {
		return nil, fmt.Errorf("no foundry job %s", id)
	}
	return j, nil
}

func (s *JobStore) List() []*Job {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Job, 0, len(s.jobs))
	for _, j := range s.jobs {
		out = append(out, j)
	}
	return out
}
