package wiring

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/redbotster/nanobots/internal/runner"
)

// The composition, not the pruning.
//
// runner.PruneWorkDirs is tested where it lives, including its refusal to
// delete anything not shaped like a run id. What is only decided here is
// *what to keep*: the set is built from the store's own list, and if it
// ever came back empty — a history directory that failed to read, a
// refactor that moved the List() call — this would delete the workspace of
// every run the app still shows, at startup, with a cheerful log line
// saying how many.
//
// The pruning exists because 266 workspaces and 14MB accumulated against
// 200 retained runs on a real machine. That is worth keeping bounded, and
// worth never over-reaching.
func TestBuildRunStoreKeepsTheWorkspacesOfRunsItStillHas(t *testing.T) {
	base := t.TempDir()
	paths := Paths{
		DBPath:     filepath.Join(base, "nanobots.db"),
		HistoryDir: filepath.Join(base, "history"),
		RunWorkDir: filepath.Join(base, "runs"),
	}
	if err := os.MkdirAll(paths.HistoryDir, 0o755); err != nil {
		t.Fatal(err)
	}

	const kept = "11111111-1111-4111-8111-111111111111"
	const forgotten = "22222222-2222-4222-8222-222222222222"

	// One run still in history, written the way the store writes them.
	snapshot := `{"id":"` + kept + `","swarm_name":"probe","status":"succeeded",` +
		`"started_at":"2026-09-17T10:00:00Z","finished_at":"2026-09-17T10:00:05Z"}`
	if err := os.WriteFile(filepath.Join(paths.HistoryDir, kept+".json"), []byte(snapshot), 0o644); err != nil {
		t.Fatal(err)
	}

	// A workspace for each, plus something that is not a run id at all.
	for _, dir := range []string{kept, forgotten, "not-a-run-id"} {
		if err := os.MkdirAll(filepath.Join(paths.RunWorkDir, dir, "bot"), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	db, err := OpenStateDB(paths)
	if err != nil {
		t.Fatal(err)
	}
	var logged []string
	store := BuildRunStore(db, paths, func(f string, a ...any) { logged = append(logged, f) })
	if store == nil {
		t.Fatal("no store")
	}
	if len(store.List()) != 1 {
		t.Fatalf("history holds %d runs, want the one that was written", len(store.List()))
	}

	exists := func(name string) bool {
		_, err := os.Stat(filepath.Join(paths.RunWorkDir, name))
		return err == nil
	}
	if !exists(kept) {
		t.Error("the workspace of a run the app still lists was deleted")
	}
	if exists(forgotten) {
		t.Error("the workspace of a run history has forgotten was left behind")
	}
	if !exists("not-a-run-id") {
		t.Error("something that is not a run id was deleted from the work directory")
	}
	if len(logged) == 0 {
		t.Error("a startup that deleted a directory said nothing about it")
	}
}

// A first start has no work directory at all, and that is not a fault.
func TestBuildRunStoreOnAFreshMachine(t *testing.T) {
	base := t.TempDir()
	paths := Paths{
		DBPath:     filepath.Join(base, "nanobots.db"),
		HistoryDir: filepath.Join(base, "history"),
		RunWorkDir: filepath.Join(base, "runs"),
	}
	db, err := OpenStateDB(paths)
	if err != nil {
		t.Fatal(err)
	}
	var logged []string
	store := BuildRunStore(db, paths, func(f string, a ...any) { logged = append(logged, f) })
	if store == nil || len(store.List()) != 0 {
		t.Fatal("a fresh machine did not produce an empty store")
	}
	for _, l := range logged {
		if strings.Contains(l, "cleaned") {
			t.Errorf("a fresh machine reported cleaning something: %q", l)
		}
	}
}

// The address a container is told to call back on is resolved from inside
// that container, where localhost is the container itself. Getting this
// wrong breaks every containerised bot's first callback, and only in a
// container — it would look fine in every in-process run.
func TestBuildOrchestratorPointsContainersAtTheHost(t *testing.T) {
	orch := BuildOrchestrator(
		OrchestratorOpts{RepoRoot: "/repo", BotsDir: "/repo/bots", CallbackPort: "7474"},
		Paths{StateDir: "/state", RunWorkDir: "/runs", BlobDir: "/blobs"},
		nil, ServiceConfigs{}, nil, runner.NewCallbackRegistry(),
	)
	if got := orch.CallbackAddr; got != "http://host.docker.internal:7474" {
		t.Errorf("CallbackAddr = %q — a container cannot reach the daemon at localhost", got)
	}
	if orch.Callbacks == nil {
		t.Error("no callback registry, so no callback could ever be authenticated")
	}
	if orch.BotsDir != "/repo/bots" || orch.AgentStateDir != "/state" || orch.BlobDir != "/blobs" {
		t.Errorf("paths did not survive the wiring: %+v", orch)
	}
}

// Everything that persists lives under one directory, and the one the
// runner writes into on every single run is created up front — a run that
// fails on a missing workspace fails after the model call it already paid
// for.
func TestResolvePathsCreatesTheWorkDirUnderHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	paths, err := ResolvePaths()
	if err != nil {
		t.Fatalf("ResolvePaths: %v", err)
	}
	if _, err := os.Stat(paths.RunWorkDir); err != nil {
		t.Errorf("the run work directory was not created: %v", err)
	}
	for name, p := range map[string]string{
		"state":   paths.StateDir,
		"blobs":   paths.BlobDir,
		"runs":    paths.RunWorkDir,
		"foundry": paths.FoundryWorkDir,
		"history": paths.HistoryDir,
		"memory":  paths.MemoryDir,
	} {
		if !strings.HasPrefix(p, home) {
			t.Errorf("%s path %q is outside the home directory", name, p)
		}
	}
}

// The same question as the work directories, one store over: what is kept.
//
// Blobs are the file contents a run produced — a PDF, a chart, a downloaded
// attachment — and they were the last unbounded thing under ~/.nanobots.
// Pruning them is a delete loop, so what matters is that the keep set comes
// from the runs the app still lists. Empty means deleting the download link
// of every run in the UI, at startup, while reporting how many megabytes it
// freed.
func TestBuildRunStoreKeepsBlobsAKeptRunStillShows(t *testing.T) {
	base := t.TempDir()
	paths := Paths{
		DBPath:     filepath.Join(base, "nanobots.db"),
		HistoryDir: filepath.Join(base, "history"),
		RunWorkDir: filepath.Join(base, "runs"),
		BlobDir:    filepath.Join(base, "blobs"),
	}
	if err := os.MkdirAll(filepath.Join(paths.BlobDir, "sha256"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(paths.HistoryDir, 0o755); err != nil {
		t.Fatal(err)
	}

	used := strings.Repeat("a", 64)
	orphan := strings.Repeat("b", 64)
	for _, d := range []string{used, orphan} {
		if err := os.WriteFile(filepath.Join(paths.BlobDir, "sha256", d), []byte("bytes"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	// One run still in history, whose output points at `used`.
	snapshot := `{"id":"11111111-1111-4111-8111-111111111111","swarm_name":"probe",` +
		`"status":"succeeded","started_at":"2026-09-17T10:00:00Z",` +
		`"outputs":{"renderer":{"pdf":{"uri":"nbf://sha256/` + used + `","mime":"application/pdf"}}}}`
	if err := os.WriteFile(filepath.Join(paths.HistoryDir, "11111111-1111-4111-8111-111111111111.json"),
		[]byte(snapshot), 0o644); err != nil {
		t.Fatal(err)
	}

	db, err := OpenStateDB(paths)
	if err != nil {
		t.Fatal(err)
	}
	BuildRunStore(db, paths, nil)

	exists := func(d string) bool {
		_, err := os.Stat(filepath.Join(paths.BlobDir, "sha256", d))
		return err == nil
	}
	if !exists(used) {
		t.Error("startup deleted the file a run in history still shows as a download")
	}
	if exists(orphan) {
		t.Error("a file no run refers to survived")
	}
}
