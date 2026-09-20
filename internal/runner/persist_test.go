package runner

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/redbotster/nanobots/internal/statedb"
)

// newTestStore opens a fresh *sql.DB handle (matching what a real restart
// does) colocated with dir, treating dir as the legacy JSON history
// directory too — every test here already writes its hand-built JSON
// snapshots straight into dir, and the migration path picks them up from
// there exactly once, the same way a real upgrade would.
func newTestStore(t *testing.T, dir string) (*RunStore, error) {
	t.Helper()
	db, err := statedb.Open(filepath.Join(dir, "nanobots.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return NewPersistentRunStore(db, dir, nil)
}

func TestRunSurvivesARestart(t *testing.T) {
	dir := t.TempDir()

	// First "process": run something to completion.
	store, err := newTestStore(t, dir)
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
	reloaded, err := newTestStore(t, dir)
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
	store, _ := newTestStore(t, dir)
	run := NewRun("bookkeeping-assistant")
	store.Add(run)
	run.SetError(errors.New("build harness image: docker daemon not reachable"))
	run.SetStatus(StatusFailed)

	reloaded, _ := newTestStore(t, dir)
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

	reloaded, _ := newTestStore(t, dir)
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

// Migration reads the whole JSON directory in one pass, so this has to be
// proven at migration time — both files need to already be there before the
// store (and therefore the one-shot migration) is ever constructed.
func TestUnreadableRunDoesNotCostYouTheOthers(t *testing.T) {
	dir := t.TempDir()
	good := NewRun("content-engine")
	good.SetStatus(StatusSucceeded)
	if err := writeSnapshot(dir, good); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "corrupt.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	store, err := newTestStore(t, dir)
	if err != nil {
		t.Fatalf("a corrupt file should be skipped, not fail the load: %v", err)
	}
	if _, err := store.Get(good.ID); err != nil {
		t.Errorf("the good run was lost alongside the corrupt one: %v", err)
	}
}

// The cap is enforced in SQLite now, not by deleting JSON files — those are
// the one-time migration source and are never written to or pruned again
// (see NewPersistentRunStore's doc comment). What has to stay bounded is
// the runs table itself, or "1,000 runs" in docs/run-history.md's own
// measurement would silently become "however many ever ran."
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

	store, err := newTestStore(t, dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if n := len(store.List()); n != MaxPersistedRuns {
		t.Errorf("loaded %d runs, want the cap of %d", n, MaxPersistedRuns)
	}
	if _, err := store.Get(newest); err != nil {
		t.Error("pruning dropped the newest run; it must drop the oldest")
	}
	if _, err := store.Get(oldest); err == nil {
		t.Error("the oldest run is still in the store; the table would grow forever")
	}
	if _, err := os.Stat(filepath.Join(dir, oldest+".json")); err != nil {
		t.Error("the oldest run's JSON file was deleted — migration must never remove the source it migrated from")
	}
}

// The in-memory map used to only grow: nothing ever removed a finished run
// from it for as long as the process kept running, which for a swarm on a
// 30-minute schedule is ~17,500 runs a year, each holding its full log and
// every bot's outputs. evictOldestBeyondCap caps the live map the same way
// pruneSQL already caps the table — but only ever a *finished* run, and
// never one still in progress, however old it started.
func TestEvictionCapsTheLiveMapButNeverARunningOne(t *testing.T) {
	dir := t.TempDir()
	store, err := newTestStore(t, dir)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	base := time.Now().Add(-time.Hour)

	// Started before any of the finished runs below, so an eviction that
	// only looked at age rather than status would wrongly pick this one.
	running := NewRun("swarm")
	running.StartedAt = base
	store.Add(running)
	running.SetStatus(StatusRunning)

	var oldestFinished, newestFinished string
	for i := 0; i < MaxPersistedRuns+5; i++ {
		r := NewRun("swarm")
		r.StartedAt = base.Add(time.Duration(i+1) * time.Minute)
		store.Add(r)
		r.SetStatus(StatusSucceeded)
		if i == 0 {
			oldestFinished = r.ID
		}
		newestFinished = r.ID
	}

	store.mu.RLock()
	inMemory := len(store.runs)
	_, runningStillInMemory := store.runs[running.ID]
	_, oldestStillInMemory := store.runs[oldestFinished]
	store.mu.RUnlock()

	// +1: the still-running run is never a candidate, so it sits on top of
	// the cap rather than counting against it.
	if inMemory > MaxPersistedRuns+1 {
		t.Errorf("in-memory run count = %d, want capped at %d (+1 for the running one)", inMemory, MaxPersistedRuns)
	}
	if !runningStillInMemory {
		t.Error("a still-running run must never be evicted, however old it started")
	}
	if oldestStillInMemory {
		t.Error("the oldest finished run is still in memory — the map would grow forever")
	}

	// Dropped from memory, but not lost: Get falls back to the database
	// for anything the map no longer holds.
	if _, err := store.Get(oldestFinished); err != nil {
		t.Errorf("an evicted run should still be readable from the database: %v", err)
	}
	if _, err := store.Get(newestFinished); err != nil {
		t.Errorf("the newest finished run should still be reachable: %v", err)
	}
}

// A run reconstructed from the database (evicted from memory, or loaded
// fresh after a restart via Get rather than the bulk startup load) must
// behave like the honest, read-only history it is — not silently claim to
// still be live.
func TestARunLoadedFromTheDatabaseByGetIsAFullReconstruction(t *testing.T) {
	dir := t.TempDir()
	store, err := newTestStore(t, dir)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	r := NewRun("daily-email-recap")
	r.TriggeredBy = "schedule"
	store.Add(r)
	r.Log("triage", "classify", "sorted %d messages", 12)
	r.SetBotOutputs("triage", map[string]any{"urgent": float64(3)})
	r.SetStatus(StatusSucceeded)

	// Force eviction of this one run without creating a thousand others:
	// same lock discipline evictOldestBeyondCap uses, applied directly.
	store.mu.Lock()
	delete(store.runs, r.ID)
	store.mu.Unlock()

	got, err := store.Get(r.ID)
	if err != nil {
		t.Fatalf("Get after eviction: %v", err)
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

func TestGetOfAnUnknownRunFailsEvenWithADatabaseConfigured(t *testing.T) {
	dir := t.TempDir()
	store, err := newTestStore(t, dir)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	if _, err := store.Get("no-such-run"); err == nil {
		t.Error("expected an error for a run id that exists nowhere")
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
			store, _ := newTestStore(t, dir)
			run := NewRun("swarm")
			store.Add(run)
			tc.apply(run, errors.New("docker daemon not reachable"))

			reloaded, _ := newTestStore(t, dir)
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

// The last unbounded store under ~/.nanobots.
//
// History is capped and work directories were bounded once the 266 of them
// were noticed, but every PDF, chart and downloaded attachment a run ever
// produced stayed on disk for good — reachable from nothing the moment its
// run aged out of the 200 kept.
//
// The delete is the dangerous half, so the cases that matter are the ones
// where it must NOT happen: a file a kept run still shows as a download, a
// file only a captured fixture refers to, and anything in the store that is
// not shaped like a digest at all.
func TestPruneBlobsKeepsWhatAKeptRunStillPointsAt(t *testing.T) {
	dir := t.TempDir()
	sha := filepath.Join(dir, "sha256")
	if err := os.MkdirAll(sha, 0o755); err != nil {
		t.Fatal(err)
	}
	const (
		used       = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		captured   = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
		forgotten  = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
		notADigest = "README"
	)
	for _, name := range []string{used, captured, forgotten, notADigest} {
		if err := os.WriteFile(filepath.Join(sha, name), []byte("some bytes"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	keep := map[string]bool{used: true, captured: true}
	removed, freed, err := PruneBlobs(dir, keep)
	if err != nil {
		t.Fatalf("PruneBlobs: %v", err)
	}
	if removed != 1 {
		t.Errorf("removed %d, want only the unreferenced one", removed)
	}
	if freed == 0 {
		t.Error("freed 0 bytes after deleting a file")
	}

	exists := func(n string) bool { _, err := os.Stat(filepath.Join(sha, n)); return err == nil }
	if !exists(used) {
		t.Error("deleted a file a kept run still shows as a download")
	}
	if !exists(captured) {
		t.Error("deleted a file a kept run's captured fixtures refer to")
	}
	if exists(forgotten) {
		t.Error("kept a file no run refers to")
	}
	if !exists(notADigest) {
		t.Error("deleted something that is not a blob at all")
	}
}

// A machine that has never produced a file has no blob directory, and that
// is the normal first start rather than a fault.
func TestPruneBlobsOnAFreshMachine(t *testing.T) {
	removed, freed, err := PruneBlobs(filepath.Join(t.TempDir(), "never-created"), nil)
	if err != nil || removed != 0 || freed != 0 {
		t.Errorf("PruneBlobs on a fresh machine = %d, %d, %v", removed, freed, err)
	}
}

// What counts as "still referred to" has to cover both places a digest can
// survive in history: a run's outputs, and the fixtures it captured.
func TestBlobRefsFindsEveryPlaceADigestSurvives(t *testing.T) {
	r := NewRun("probe")
	r.SetBotOutputs("renderer", map[string]any{
		"pdf":  map[string]any{"uri": "nbf://sha256/" + strings.Repeat("a", 64), "mime": "application/pdf"},
		"note": "not a blob",
		"many": []any{map[string]any{"uri": "nbf://sha256/" + strings.Repeat("b", 64)}},
	})
	r.SetCaptured("filer", map[string]any{
		"gdrive.files.download": map[string]any{"uri": "nbf://sha256/" + strings.Repeat("c", 64)},
	})

	got := map[string]bool{}
	for _, d := range BlobRefs(r) {
		got[d] = true
	}
	for _, want := range []string{strings.Repeat("a", 64), strings.Repeat("b", 64), strings.Repeat("c", 64)} {
		if !got[want] {
			t.Errorf("missed the digest %s… — pruning would delete a file still in use", want[:8])
		}
	}
	if len(got) != 3 {
		t.Errorf("found %d digests, want 3: %v", len(got), got)
	}
}
