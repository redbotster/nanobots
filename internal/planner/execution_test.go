package planner

import (
	"os"
	"path/filepath"
	"testing"
)

// v3 Phase 1's execution: override. CheckExecution is exercised through
// PlanSwarm rather than called directly, matching how CheckFallback and
// CheckLoop are tested elsewhere in this package — a real swarm+bot pair on
// disk is what a plan actually sees, and building a ResolvedSwarm by hand
// would risk testing a shape Resolve never produces.

// writeExecutionBot writes a minimal bot, optionally with execution: set on
// its own harness block — "" leaves the field out entirely, matching what
// every bot in the catalog looks like today.
func writeExecutionBot(t *testing.T, botsDir, name, harnessExecution string) {
	t.Helper()
	dir := filepath.Join(botsDir, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	execLine := ""
	if harnessExecution != "" {
		execLine = "\n    execution: " + harnessExecution
	}
	body := `apiVersion: nanobots.dev/v1alpha1
kind: Nanobot
metadata:
  name: ` + name + `
  version: 0.1.0
  description: probe
spec:
  harness:
    type: bare` + execLine + `
  ports:
    outputs:
      - name: ok
        type: string
  steps:
    - name: noop
      type: log
      value: probe
`
	if err := os.WriteFile(filepath.Join(dir, "nanobot.yaml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestNoExecutionOverridePlansClean(t *testing.T) {
	botsDir := t.TempDir()
	writeExecutionBot(t, botsDir, "worker", "")

	res := planFallback(t, botsDir, `  bots:
    - id: w
      use: worker@0.1.0
`)
	if errs := fallbackErrs(res); len(errs) > 0 {
		t.Fatalf("a bot with no execution: override was rejected: %v", errs)
	}
}

func TestExecutionContainerOnTheSwarmInstancePlansClean(t *testing.T) {
	botsDir := t.TempDir()
	writeExecutionBot(t, botsDir, "worker", "")

	res := planFallback(t, botsDir, `  bots:
    - id: w
      use: worker@0.1.0
      execution: container
`)
	if errs := fallbackErrs(res); len(errs) > 0 {
		t.Fatalf("execution: container on the swarm instance was rejected: %v", errs)
	}
}

func TestExecutionContainerOnTheBotItselfPlansClean(t *testing.T) {
	botsDir := t.TempDir()
	writeExecutionBot(t, botsDir, "worker", "container")

	res := planFallback(t, botsDir, `  bots:
    - id: w
      use: worker@0.1.0
`)
	if errs := fallbackErrs(res); len(errs) > 0 {
		t.Fatalf("execution: container on the bot's own harness was rejected: %v", errs)
	}
}

// "inprocess" is not a legal value anywhere — Harness.Execution's own doc
// comment says why: the only thing an override like that could do is force
// a browser-driving bot out of the one sandbox that isolation argument
// actually applies to.
func TestExecutionInprocessIsRejectedOnTheSwarmInstance(t *testing.T) {
	botsDir := t.TempDir()
	writeExecutionBot(t, botsDir, "worker", "")

	res := planFallback(t, botsDir, `  bots:
    - id: w
      use: worker@0.1.0
      execution: inprocess
`)
	if !containsSubstr(fallbackErrs(res), `execution: "inprocess"`) {
		t.Errorf("expected a rejection naming the illegal value, got %v", fallbackErrs(res))
	}
}

func TestExecutionInprocessIsRejectedOnTheBotItself(t *testing.T) {
	botsDir := t.TempDir()
	writeExecutionBot(t, botsDir, "worker", "inprocess")

	res := planFallback(t, botsDir, `  bots:
    - id: w
      use: worker@0.1.0
`)
	if !containsSubstr(fallbackErrs(res), `execution: "inprocess"`) {
		t.Errorf("expected a rejection naming the illegal value, got %v", fallbackErrs(res))
	}
}

func TestExecutionGarbageValueIsRejected(t *testing.T) {
	botsDir := t.TempDir()
	writeExecutionBot(t, botsDir, "worker", "")

	res := planFallback(t, botsDir, `  bots:
    - id: w
      use: worker@0.1.0
      execution: yolo
`)
	if !containsSubstr(fallbackErrs(res), `execution: "yolo"`) {
		t.Errorf("expected a rejection naming the illegal value, got %v", fallbackErrs(res))
	}
}
