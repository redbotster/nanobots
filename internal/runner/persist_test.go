package runner

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestRunSurvivesARestart(t *testing.T) {
	dir := t.TempDir()

	// First "process": run something to completion.
	store, err := NewPersistentRunStore(dir)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	run := NewRun("daily-email-recap")
	run.SwarmPath = "examples/swarms/daily-email-recap.yaml"
	run.TriggeredBy = "schedule"
	store.Add(run)
	run.SetStatus(StatusRunning)
	run.Log("triage", "classify", "sorted %d messages", 12)
	run.SetBotOutputs("triage", map[string]any{"urgent": float64(3)})
	run.SetStatus(StatusSucceeded)

	// Second "process": nothing in memory, everything from disk.
	reloaded, err := NewPersistentRunStore(dir)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	got, err := reloaded.Get(run.ID)
	if err != nil {
		t.Fatalf("the run did not survive the restart: %v", err)
	}
	if got.SwarmName != "daily-email-recap" || got.SwarmPath != run.SwarmPath {
		t.Errorf("identity lost: name=%q path=%q", got.SwarmName, got.SwarmPath)
	}
	if got.TriggeredBy != "schedule" {
		t.Errorf("TriggeredBy = %q, want schedule — a restored run should still explain itself", got.TriggeredBy)
	}
	if got.GetStatus() != StatusSucceeded {
		t.Errorf("status = %q, want succeeded", got.GetStatus())
	}
	if entries := got.LogEntries(); len(entries) != 1 || entries[0].Msg != "sorted 12 messages" {
		t.Errorf("log = %+v, want the one entry back verbatim", entries)
	}
	if out, ok := got.BotOutputs("triage"); !ok || out["urgent"] != float64(3) {
		t.Errorf("outputs = %+v (ok=%v), want the triage output back", out, ok)
	}
}

func TestFailedRunKeepsItsError(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewPersistentRunStore(dir)
	run := NewRun("bookkeeping-assistant")
	store.Add(run)
	run.SetError(errors.New("build harness image: docker daemon not reachable"))
	run.SetStatus(StatusFailed)

	reloaded, _ := NewPersistentRunStore(dir)
	got, err := reloaded.Get(run.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	// The whole point of showing "why it failed" is that it's still there
	// tomorrow, when you come back to the run and try again.
	if got.GetError() != "build harness image: docker daemon not reachable" {
		t.Errorf("error = %q, want it preserved", got.GetError())
	}
}

func TestInFlightRunIsRestoredAsFailedNotRunning(t *testing.T) {
	dir := t.TempDir()
	// Write a snapshot by hand in the state a crash would leave behind:
	// the run never reached a terminal status, so nothing ever wrote it —
	// but a future terminal-state writer, or a hand-edited file, could.
	stuck := NewRun("inbox-autopilot")
	stuck.SetStatus(StatusAwaitingApproval)
	if err := writeSnapshot(dir, stuck); err != nil {
		t.Fatalf("write: %v", err)
	}

	reloaded, _ := NewPersistentRunStore(dir)
	got, err := reloaded.Get(stuck.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.GetStatus() != StatusFailed {
		t.Errorf("status = %q, want failed — nothing can approve a run whose process is gone", got.GetStatus())
	}
	if got.GetError() == "" {
		t.Error("a run killed by a restart should say that's what happened")
	}
	if got.GetFinishedAt().IsZero() {
		t.Error("a restored dead run needs a finished time, or it sorts and renders as if still going")
	}
}

func TestUnreadableRunDoesNotCostYouTheOthers(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewPersistentRunStore(dir)
	good := NewRun("content-engine")
	store.Add(good)
	good.SetStatus(StatusSucceeded)

	if err := os.WriteFile(filepath.Join(dir, "corrupt.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	reloaded, err := NewPersistentRunStore(dir)
	if err != nil {
		t.Fatalf("a corrupt file should be skipped, not fail the load: %v", err)
	}
	if _, err := reloaded.Get(good.ID); err != nil {
		t.Errorf("the good run was lost alongside the corrupt one: %v", err)
	}
}

func TestHistoryIsPrunedToTheCap(t *testing.T) {
	dir := t.TempDir()
	base := time.Now().Add(-time.Hour)
	var newest, oldest string
	for i := 0; i < MaxPersistedRuns+5; i++ {
		r := NewRun("swarm")
		r.StartedAt = base.Add(time.Duration(i) * time.Minute)
		r.SetStatus(StatusSucceeded)
		if err := writeSnapshot(dir, r); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			oldest = r.ID
		}
		newest = r.ID
	}

	store, err := NewPersistentRunStore(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if n := len(store.List()); n != MaxPersistedRuns {
		t.Errorf("loaded %d runs, want the cap of %d", n, MaxPersistedRuns)
	}
	if _, err := store.Get(newest); err != nil {
		t.Error("pruning dropped the newest run; it must drop the oldest")
	}
	if _, err := os.Stat(filepath.Join(dir, oldest+".json")); !os.IsNotExist(err) {
		t.Error("the oldest run's file is still on disk; the directory would grow forever")
	}
}

func TestStoreWithoutADirWritesNothing(t *testing.T) {
	dir := t.TempDir()
	store := NewRunStore() // no Dir — what every non-persisting test gets
	run := NewRun("swarm")
	store.Add(run)
	run.SetStatus(StatusSucceeded)

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("an in-memory store touched the disk: %v", entries)
	}
}

// Regression: the terminal status is what triggers the write to history, so
// an error set *after* the status used to persist a failed run with a blank
// reason — the exact thing "Why it failed" exists to show. Both orders must
// end up with the reason on disk.
func TestErrorPersistsWhicheverOrderItIsSetIn(t *testing.T) {
	for _, tc := range []struct {
		name  string
		apply func(*Run, error)
	}{
		{"error then status", func(r *Run, e error) { r.SetError(e); r.SetStatus(StatusFailed) }},
		{"status then error", func(r *Run, e error) { r.SetStatus(StatusFailed); r.SetError(e) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			store, _ := NewPersistentRunStore(dir)
			run := NewRun("swarm")
			store.Add(run)
			tc.apply(run, errors.New("docker daemon not reachable"))

			reloaded, _ := NewPersistentRunStore(dir)
			got, err := reloaded.Get(run.ID)
			if err != nil {
				t.Fatalf("get: %v", err)
			}
			if got.GetError() != "docker daemon not reachable" {
				t.Errorf("persisted error = %q, want the reason", got.GetError())
			}
		})
	}
}

// The history directory has been bounded since it existed. The per-run
// workspaces beside it never were, and nothing removed one — 266
// directories and 14MB against 200 retained runs on the machine this was
// found on, 66 of them belonging to runs that had aged out of history and
// were reachable from nowhere.
func TestPruneWorkDirsRemovesOnlyWhatHistoryHasForgotten(t *testing.T) {
	dir := t.TempDir()

	kept := uuid.NewString()
	forgotten := uuid.NewString()
	alsoForgotten := uuid.NewString()
	for _, id := range []string{kept, forgotten, alsoForgotten} {
		if err := os.MkdirAll(filepath.Join(dir, id, "somebot"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, id, "somebot", "inputs.json"), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	removed, err := PruneWorkDirs(dir, map[string]bool{kept: true})
	if err != nil {
		t.Fatal(err)
	}
	if removed != 2 {
		t.Errorf("removed = %d, want 2", removed)
	}
	if _, err := os.Stat(filepath.Join(dir, kept)); err != nil {
		t.Errorf("deleted the workspace of a run still in history: %v", err)
	}
	for _, gone := range []string{forgotten, alsoForgotten} {
		if _, err := os.Stat(filepath.Join(dir, gone)); !os.IsNotExist(err) {
			t.Errorf("workspace %s survived: %v", gone, err)
		}
	}
}

// Anything not shaped like a run id is left alone. This calls os.RemoveAll
// on names read off a disk listing, so it only ever removes things it can
// recognise as its own.
func TestPruneWorkDirsIgnoresAnythingThatIsNotARunID(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"not-a-uuid", "blobs", ".DS_Store_dir"} {
		if err := os.MkdirAll(filepath.Join(dir, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// A plain file alongside them must also survive.
	if err := os.WriteFile(filepath.Join(dir, "README"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	removed, err := PruneWorkDirs(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 0 {
		t.Errorf("removed = %d, want 0 — nothing here is a run workspace", removed)
	}
	for _, name := range []string{"not-a-uuid", "blobs", ".DS_Store_dir", "README"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("%s was removed: %v", name, err)
		}
	}
}

// First start, before any run has happened.
func TestPruneWorkDirsIsFineWithNoDirectoryAtAll(t *testing.T) {
	removed, err := PruneWorkDirs(filepath.Join(t.TempDir(), "never-created"), nil)
	if err != nil {
		t.Errorf("err = %v, want nil on first start", err)
	}
	if removed != 0 {
		t.Errorf("removed = %d", removed)
	}
}

// A run is written to history the moment it finishes and read back from
// there forever after, so anything the snapshot drops is a fact the app has
// for one process lifetime and then loses.
//
// Found by measuring rather than by reading: the scheduler's 08:15 and
// 09:15 meeting-to-action runs had "nothing to do" in their logs and an
// empty nothing_to_do field, because the daemon had restarted between the
// run and the look. The Runs page was back to calling them "succeeded" —
// which is the whole thing that field was added to prevent.
//
// StoppedByUser is the older and worse one: a run you ended yourself reads
// as a plain red "failed", the first example in CLAUDE.md's list of honesty
// bugs, arriving again through the back door.
func TestHistoryKeepsWhatMakesAStatusMeanSomething(t *testing.T) {
	dir := t.TempDir()

	quiet := NewRun("meeting-to-action")
	quiet.TriggeredBy = "schedule"
	quiet.SetNothingToDo("no new file in this folder since the last run")
	quiet.SetStatus(StatusSucceeded)

	stopped := NewRun("morning-brief")
	stopped.Stop()
	stopped.SetError(ErrStopped)
	stopped.SetStatus(StatusFailed)

	declined := NewRun("get-paid")
	declined.DeclinedByUser = true
	declined.SetError(errTest(`step "gate": not approved (decided_by=cli)`))
	declined.SetStatus(StatusFailed)

	for _, r := range []*Run{quiet, stopped, declined} {
		if err := writeSnapshot(dir, r); err != nil {
			t.Fatalf("writeSnapshot: %v", err)
		}
	}

	restored, err := loadSnapshots(dir)
	if err != nil {
		t.Fatalf("loadSnapshots: %v", err)
	}
	byName := map[string]*Run{}
	for _, r := range restored {
		byName[r.SwarmName] = r
	}
	if len(byName) != 3 {
		t.Fatalf("restored %d runs, want 3", len(byName))
	}

	if got := byName["meeting-to-action"].GetNothingToDo(); got != "no new file in this folder since the last run" {
		t.Errorf("a quiet watch run came back as an ordinary success (nothing_to_do = %q)", got)
	}
	if !byName["morning-brief"].WasStoppedByUser() {
		t.Error("a run the user stopped came back looking like one that broke")
	}
	if !byName["get-paid"].WasDeclinedByUser() {
		t.Error("a declined approval came back looking like a fault rather than a decision")
	}
}
