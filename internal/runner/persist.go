package runner

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Run history used to live only in memory, so restarting nanobotd — which
// you do every time you rebuild — threw away every run you'd made. The Runs
// page said so in its own copy ("history doesn't persist across a restart
// yet"), which is a bad thing for a product to have to apologize for.
//
// One JSON file per run, written when the run reaches a terminal state.
// Deliberately not a database: a run is small, self-contained, and only ever
// written once, so a directory of files is the whole feature — no schema, no
// migrations, and you can read one with `cat`.

// snapshot is a run flattened for disk. It exists rather than marshalling
// *Run directly because Run's log/outputs live behind a mutex in unexported
// fields, and because the on-disk shape should be free to lag the in-memory
// one.
type snapshot struct {
	ID          string                    `json:"id"`
	SwarmName   string                    `json:"swarm_name"`
	SwarmPath   string                    `json:"swarm_path,omitempty"`
	Status      RunStatus                 `json:"status"`
	StartedAt   time.Time                 `json:"started_at"`
	FinishedAt  time.Time                 `json:"finished_at,omitempty"`
	Error       string                    `json:"error,omitempty"`
	TriggeredBy string                    `json:"triggered_by"`
	Tolerated   []ToleratedFailure        `json:"tolerated,omitempty"`
	Log         []LogEntry                `json:"log"`
	Outputs     map[string]map[string]any `json:"outputs"`
}

func snapshotOf(r *Run) snapshot {
	return snapshot{
		ID:          r.ID,
		SwarmName:   r.SwarmName,
		SwarmPath:   r.SwarmPath,
		Status:      r.GetStatus(),
		StartedAt:   r.StartedAt,
		FinishedAt:  r.GetFinishedAt(),
		Error:       r.GetError(),
		Tolerated:   r.GetTolerated(),
		TriggeredBy: r.TriggeredBy,
		Log:         r.LogEntries(),
		Outputs:     r.AllOutputs(),
	}
}

func (s snapshot) toRun() *Run {
	r := &Run{
		ID:          s.ID,
		SwarmName:   s.SwarmName,
		SwarmPath:   s.SwarmPath,
		Status:      s.Status,
		StartedAt:   s.StartedAt,
		FinishedAt:  s.FinishedAt,
		Error:       s.Error,
		Tolerated:   s.Tolerated,
		TriggeredBy: s.TriggeredBy,
		log:         s.Log,
		outputs:     s.Outputs,
		approvals:   map[string]*PendingApproval{},
		subscribers: map[chan LogEntry]bool{},
	}
	if r.outputs == nil {
		r.outputs = map[string]map[string]any{}
	}
	// A run that was still going when the process died is not resumable —
	// its goroutine, its containers, and anyone waiting on an approval are
	// all gone. Saying so is more honest than restoring it as "running"
	// forever, which is what a naive reload would do.
	if r.Status == StatusRunning || r.Status == StatusPending || r.Status == StatusAwaitingApproval {
		r.Status = StatusFailed
		if r.Error == "" {
			r.Error = "nanobotd restarted while this run was in progress"
		}
		if r.FinishedAt.IsZero() {
			r.FinishedAt = r.StartedAt
		}
	}
	return r
}

// writeSnapshot persists one run, atomically — a torn file from a crash
// mid-write would fail to parse on the next load and lose the run anyway,
// so the temp-file-then-rename is worth the extra syscall.
func writeSnapshot(dir string, r *Run) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(snapshotOf(r))
	if err != nil {
		return err
	}
	final := filepath.Join(dir, r.ID+".json")
	tmp, err := os.CreateTemp(dir, r.ID+".*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), final)
}

// loadSnapshots reads every run in dir, newest first. A file that won't
// parse is skipped rather than failing the whole load: one bad run must not
// cost you the other two hundred.
func loadSnapshots(dir string) ([]*Run, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil // nothing has ever run on this machine
	}
	if err != nil {
		return nil, err
	}
	var runs []*Run
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var s snapshot
		if err := json.Unmarshal(data, &s); err != nil || s.ID == "" {
			continue
		}
		runs = append(runs, s.toRun())
	}
	sort.Slice(runs, func(i, j int) bool { return runs[i].StartedAt.After(runs[j].StartedAt) })
	return runs, nil
}

// prune deletes all but the newest keep runs from disk. Called after a load
// so history is bounded without anyone having to think about it; runs are a
// few KB each, so the cap is about not letting a directory grow unbounded
// for years, not about disk pressure.
func prune(dir string, runs []*Run, keep int) {
	for _, r := range runs[min(keep, len(runs)):] {
		os.Remove(filepath.Join(dir, r.ID+".json"))
	}
}

// DefaultHistoryDir is where nanobotd keeps run history — a sibling of the
// blobs/ and runs/ (container scratch) directories it already owns.
func DefaultHistoryDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate home directory for run history: %w", err)
	}
	return filepath.Join(home, ".nanobots", "history"), nil
}
