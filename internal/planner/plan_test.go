package planner

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/redbotster/nanobots/internal/schema"
)

// repoRoot finds the repo root from this test file's own location, so the
// test works regardless of the working directory `go test` is invoked from.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// this file: internal/planner/plan_test.go
	return filepath.Join(filepath.Dir(file), "..", "..")
}

func TestPlanDailyEmailRecap(t *testing.T) {
	root := repoRoot(t)
	swarmPath := filepath.Join(root, "examples", "swarms", "daily-email-recap.yaml")
	botsDir := filepath.Join(root, "bots")

	result, err := Plan(swarmPath, botsDir)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if !result.OK() {
		t.Fatalf("expected plan to be OK, got:\n%s", result.Report())
	}
	if len(result.Snaps) != 2 {
		t.Fatalf("expected 2 snaps, got %d", len(result.Snaps))
	}
	for _, s := range result.Snaps {
		if !s.OK {
			t.Errorf("snap %s -> %s failed: %v", s.Snap.From, s.Snap.To, s.Err)
		}
	}

	order, err := result.DAG.TopoSort()
	if err != nil {
		t.Fatalf("TopoSort: %v", err)
	}
	if len(order) != 2 || order[0] != "recap" || order[1] != "mailer" {
		t.Errorf("expected run order [recap mailer], got %v", order)
	}
}

// TestPlanAllExampleSwarms sweeps every swarm under examples/swarms/, so a
// new or edited swarm that stops type-checking fails a test immediately
// instead of only being caught by hand-running `nanobots plan` — the same
// discovery pattern internal/contract's allBotIDs uses for bots.
func TestPlanAllExampleSwarms(t *testing.T) {
	root := repoRoot(t)
	botsDir := filepath.Join(root, "bots")
	swarmsDir := filepath.Join(root, "examples", "swarms")

	entries, err := os.ReadDir(swarmsDir)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		found = true
		name := e.Name()
		t.Run(name, func(t *testing.T) {
			result, err := Plan(filepath.Join(swarmsDir, name), botsDir)
			if err != nil {
				t.Fatalf("Plan: %v", err)
			}
			if !result.OK() {
				t.Fatalf("swarm %s failed to type-check:\n%s", name, result.Report())
			}
		})
	}
	if !found {
		t.Fatal("no swarms found under examples/swarms/ — did the test find the wrong repo root?")
	}
}

func TestBuildDAGDetectsCycle(t *testing.T) {
	rs := &ResolvedSwarm{
		Swarm: &schema.Nanoswarm{
			Spec: schema.NanoswarmSpec{
				Bots: []schema.BotRef{{ID: "a"}, {ID: "b"}},
				Snaps: []schema.Snap{
					{From: "a.out", To: "b.in"},
					{From: "b.out", To: "a.in"},
				},
			},
		},
		Bots: map[string]*ResolvedBot{
			"a": {Ref: schema.BotRef{ID: "a"}},
			"b": {Ref: schema.BotRef{ID: "b"}},
		},
	}
	if _, err := BuildDAG(rs); err == nil {
		t.Fatal("expected a cycle error, got nil")
	}
}

func TestTypeCheckSnapsRejectsMismatch(t *testing.T) {
	root := repoRoot(t)
	recap, err := schema.LoadNanobot(filepath.Join(root, "bots", "recap-emails-to-pdf", "nanobot.yaml"))
	if err != nil {
		t.Fatalf("LoadNanobot: %v", err)
	}
	mailer, err := schema.LoadNanobot(filepath.Join(root, "bots", "email-drive-file", "nanobot.yaml"))
	if err != nil {
		t.Fatalf("LoadNanobot: %v", err)
	}
	rs := &ResolvedSwarm{
		Swarm: &schema.Nanoswarm{
			Spec: schema.NanoswarmSpec{
				Snaps: []schema.Snap{
					// recap_json is a json port; mailer.to is a string port — not assignable.
					{From: "recap.recap_json", To: "mailer.to"},
				},
			},
		},
		Bots: map[string]*ResolvedBot{
			"recap":  {Nanobot: recap},
			"mailer": {Nanobot: mailer},
		},
	}
	checks := TypeCheckSnaps(rs)
	if len(checks) != 1 {
		t.Fatalf("expected 1 check, got %d", len(checks))
	}
	if checks[0].OK {
		t.Fatal("expected type mismatch to fail, but it passed")
	}
}
