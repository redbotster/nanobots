package runner

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
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
	ID          string             `json:"id"`
	SwarmName   string             `json:"swarm_name"`
	SwarmPath   string             `json:"swarm_path,omitempty"`
	Status      RunStatus          `json:"status"`
	StartedAt   time.Time          `json:"started_at"`
	FinishedAt  time.Time          `json:"finished_at,omitempty"`
	Error       string             `json:"error,omitempty"`
	TriggeredBy string             `json:"triggered_by"`
	Tolerated   []ToleratedFailure `json:"tolerated,omitempty"`
	// Captured is what each bot's run would produce as fixtures. Persisted
	// so "turn this run into test data" still works after a restart —
	// otherwise the button quietly disappears from every run in history.
	Captured map[string]map[string]any `json:"captured,omitempty"`
	// DemoServices are "<bot>.<service>" pairs served from fixtures, kept
	// so a run in history still says its results were invented.
	DemoServices []string `json:"demo_services,omitempty"`
	// The three facts that make a terminal status mean something, and that
	// this snapshot used to drop on the floor.
	//
	// A run is written here the moment it finishes and read back from here
	// forever after, so anything missing is a fact the app has for one
	// process lifetime and then loses. Each of these turns into a lie when
	// it goes:
	//
	//	StoppedByUser  a run you ended yourself reads as a plain red
	//	               "failed" — the first example in CLAUDE.md's list of
	//	               honesty bugs, arriving again through the back door.
	//	NothingToDo    a watch that correctly found nothing reads as
	//	               another "succeeded", which is the entire thing the
	//	               field was added to prevent.
	//	DeclinedByUser a declined approval stops looking like a decision.
	StoppedByUser  bool                      `json:"stopped_by_user,omitempty"`
	DeclinedByUser bool                      `json:"declined_by_user,omitempty"`
	NothingToDo    string                    `json:"nothing_to_do,omitempty"`
	Log            []LogEntry                `json:"log"`
	Outputs        map[string]map[string]any `json:"outputs"`
}

func snapshotOf(r *Run) snapshot {
	return snapshot{
		ID:             r.ID,
		SwarmName:      r.SwarmName,
		SwarmPath:      r.SwarmPath,
		Status:         r.GetStatus(),
		StartedAt:      r.StartedAt,
		FinishedAt:     r.GetFinishedAt(),
		Error:          r.GetError(),
		Tolerated:      r.GetTolerated(),
		Captured:       cappedCaptures(r.Captured()),
		DemoServices:   r.DemoServices(),
		TriggeredBy:    r.TriggeredBy,
		StoppedByUser:  r.WasStoppedByUser(),
		DeclinedByUser: r.WasDeclinedByUser(),
		NothingToDo:    r.GetNothingToDo(),
		Log:            r.LogEntries(),
		Outputs:        r.AllOutputs(),
	}
}

func (s snapshot) toRun() *Run {
	r := &Run{
		ID:             s.ID,
		SwarmName:      s.SwarmName,
		SwarmPath:      s.SwarmPath,
		Status:         s.Status,
		StartedAt:      s.StartedAt,
		FinishedAt:     s.FinishedAt,
		Error:          s.Error,
		Tolerated:      s.Tolerated,
		captured:       s.Captured,
		demoServices:   demoSet(s.DemoServices),
		TriggeredBy:    s.TriggeredBy,
		StoppedByUser:  s.StoppedByUser,
		DeclinedByUser: s.DeclinedByUser,
		NothingToDo:    s.NothingToDo,
		log:            s.Log,
		outputs:        s.Outputs,
		approvals:      map[string]*PendingApproval{},
		subscribers:    map[chan LogEntry]bool{},
	}
	if r.outputs == nil {
		r.outputs = map[string]map[string]any{}
	}
	// A run that was still going when the process died is not resumable —
	// its goroutine, its containers, and anyone waiting on an approval are
	// all gone. Saying so is more honest than restoring it as "running"
	// forever, which is what a naive reload would do.
	if r.Status == StatusRunning || r.Status == StatusPending || r.Status == StatusAwaitingApproval ||
		r.Status == StatusAwaitingUnlock {
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

// maxCapturedFixture bounds one recorded fixture on disk. A fixture is
// meant to be readable test data someone commits; a megabyte of scraped
// HTML from a web.fetch is neither, and would bloat every run snapshot in
// history to keep it.
const maxCapturedFixture = 256 << 10

func cappedCaptures(in map[string]map[string]any) map[string]map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := map[string]map[string]any{}
	for bot, fixtures := range in {
		kept := map[string]any{}
		for name, v := range fixtures {
			if raw, err := json.Marshal(v); err == nil && len(raw) <= maxCapturedFixture {
				kept[name] = v
			}
		}
		if len(kept) > 0 {
			out[bot] = kept
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func demoSet(in []string) map[string]bool {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]bool, len(in))
	for _, k := range in {
		out[k] = true
	}
	return out
}

// PruneWorkDirs deletes per-run container workspaces that no run in history
// can refer to any more.
//
// The history directory has been bounded since it existed
// (MaxPersistedRuns, pruned on load). The *work* directory never was, and
// nothing anywhere removed one: on this machine it had reached 266
// directories and 14MB against 200 retained runs, so 66 of them belonged to
// runs that no longer exist in any list, readable by nothing and reachable
// from nowhere. One directory per run, forever, on a machine running
// sixteen scheduled swarms.
//
// Keyed to the runs history actually kept rather than to an age or a count
// of its own, so there is one retention rule in this package instead of two
// that can disagree. A run you can still open in the UI keeps its
// workspace; a run that has aged out of history loses it at the same
// moment.
//
// Called at startup, where nothing is running and no container holds a bind
// mount into any of these. Failures are counted and returned, never fatal:
// a workspace that cannot be deleted is untidy, and refusing to start over
// it would be worse than the mess.
func PruneWorkDirs(workDir string, keep map[string]bool) (removed int, err error) {
	entries, err := os.ReadDir(workDir)
	if err != nil {
		// No work directory yet is the normal first-start case, not a fault.
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	var failed int
	for _, e := range entries {
		if !e.IsDir() || keep[e.Name()] {
			continue
		}
		// Only things shaped like a run id. The work directory is ours, but
		// os.RemoveAll against a name that came off a disk listing deserves
		// the same "is this really one of mine" check the blob store makes
		// before reading a path (see step.FSBlobStore.Read).
		if _, uerr := uuid.Parse(e.Name()); uerr != nil {
			continue
		}
		if rerr := os.RemoveAll(filepath.Join(workDir, e.Name())); rerr != nil {
			failed++
			continue
		}
		removed++
	}
	if failed > 0 {
		return removed, fmt.Errorf("%d run workspace(s) could not be removed", failed)
	}
	return removed, nil
}

// BlobRefs collects every nbf:// digest a run still points at, so pruning
// the blob store can tell a file something still refers to from one nothing
// does.
//
// Outputs and captured fixtures both, because both survive in history and
// both render in the UI: a run detail page shows a file output as a
// download, and "turn this run into test data" replays what each bot
// produced. A digest reachable from either is a digest still in use.
func BlobRefs(r *Run) []string {
	var out []string
	walk := func(v any) {}
	walk = func(v any) {
		switch t := v.(type) {
		case string:
			if strings.HasPrefix(t, blobURIPrefix) {
				out = append(out, strings.TrimPrefix(t, blobURIPrefix))
			}
		case map[string]any:
			for _, x := range t {
				walk(x)
			}
		case []any:
			for _, x := range t {
				walk(x)
			}
		}
	}
	for _, ports := range r.AllOutputs() {
		walk(map[string]any(ports))
	}
	for _, steps := range r.Captured() {
		walk(map[string]any(steps))
	}
	return out
}

const blobURIPrefix = "nbf://sha256/"

// PruneBlobs deletes stored file contents that no run in history refers to.
//
// The last unbounded store in ~/.nanobots. History is capped, and work
// directories were bounded once the 266 of them were noticed — but every
// PDF, chart and downloaded attachment a run ever produced stayed on disk
// for good, reachable from nothing once its run aged out of the 200 kept.
//
// Same retention rule as PruneWorkDirs, for the same reason: a run you can
// still open keeps the file it produced, and a run that has aged out loses
// it at the same moment. One rule in this package, not two that can
// disagree about what "old" means.
//
// Blobs are content-addressed, so two runs that produced identical bytes
// share one file — which is exactly why this counts references across every
// kept run before deleting anything, rather than walking runs one at a
// time. Called at startup, where nothing is mid-run.
func PruneBlobs(blobDir string, keep map[string]bool) (removed int, freed int64, err error) {
	dir := filepath.Join(blobDir, "sha256")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, 0, nil // nothing has produced a file yet
		}
		return 0, 0, err
	}
	var failed int
	for _, e := range entries {
		if e.IsDir() || keep[e.Name()] {
			continue
		}
		// Only things shaped like a digest. The same instinct as
		// PruneWorkDirs' uuid.Parse: os.Remove against a name that came off
		// a disk listing deserves a check that it is really one of ours.
		if !isSHA256Hex(e.Name()) {
			continue
		}
		info, statErr := e.Info()
		path := filepath.Join(dir, e.Name())
		if rerr := os.Remove(path); rerr != nil {
			failed++
			continue
		}
		removed++
		if statErr == nil {
			freed += info.Size()
		}
	}
	if failed > 0 {
		return removed, freed, fmt.Errorf("%d blob(s) could not be removed", failed)
	}
	return removed, freed, nil
}

// isSHA256Hex is the same shape check internal/step enforces on every
// nbf:// reference before it reads one.
func isSHA256Hex(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
