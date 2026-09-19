package planner

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// v3 Phase 3's agent.loop. CheckAgentLoop is exercised through PlanSwarm,
// matching how CheckFallback/CheckLoop/CheckExecution are tested elsewhere
// in this package.

func writeAgentLoopBot(t *testing.T, botsDir, name, servicesBlock, toolsBlock string, maxIterations int) {
	t.Helper()
	dir := filepath.Join(botsDir, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `apiVersion: nanobots.dev/v1alpha1
kind: Nanobot
metadata:
  name: ` + name + `
  version: 0.1.0
  description: probe
spec:
  harness:
    type: llm
` + servicesBlock + `
  ports:
    outputs:
      - name: answer
        type: string
  steps:
    - name: research
      type: agent.loop
      goal: "go find out"
      max_iterations: ` + strconv.Itoa(maxIterations) + `
      output: answer
` + toolsBlock
	if err := os.WriteFile(filepath.Join(dir, "nanobot.yaml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestAgentLoopWithNoToolsPlansClean(t *testing.T) {
	botsDir := t.TempDir()
	writeAgentLoopBot(t, botsDir, "researcher", "", "", 5)

	res := planFallback(t, botsDir, `  bots:
    - id: r
      use: researcher@0.1.0
`)
	if errs := fallbackErrs(res); len(errs) > 0 {
		t.Fatalf("a goal-only agent.loop was rejected: %v", errs)
	}
}

func TestAgentLoopMaxIterationsOutsideRangeIsRejected(t *testing.T) {
	for _, n := range []int{0, -1, 21, 1000} {
		botsDir := t.TempDir()
		writeAgentLoopBot(t, botsDir, "researcher", "", "", n)
		res := planFallback(t, botsDir, `  bots:
    - id: r
      use: researcher@0.1.0
`)
		if !containsSubstr(fallbackErrs(res), "must be between 1 and 20") {
			t.Errorf("max_iterations=%d: expected a range error, got %v", n, fallbackErrs(res))
		}
	}
}

func TestAgentLoopBuiltinToolPlansClean(t *testing.T) {
	botsDir := t.TempDir()
	writeAgentLoopBot(t, botsDir, "researcher", "", `      tools:
        - name: fetch
          builtin: web.fetch
`, 5)
	res := planFallback(t, botsDir, `  bots:
    - id: r
      use: researcher@0.1.0
`)
	if errs := fallbackErrs(res); len(errs) > 0 {
		t.Fatalf("a valid builtin tool was rejected: %v", errs)
	}
}

func TestAgentLoopUnknownBuiltinIsRejected(t *testing.T) {
	botsDir := t.TempDir()
	writeAgentLoopBot(t, botsDir, "researcher", "", `      tools:
        - name: fetch
          builtin: transform.render
`, 5)
	res := planFallback(t, botsDir, `  bots:
    - id: r
      use: researcher@0.1.0
`)
	if !containsSubstr(fallbackErrs(res), `builtin: "transform.render"`) {
		t.Errorf("expected a rejection naming the illegal builtin, got %v", fallbackErrs(res))
	}
}

func TestAgentLoopServiceToolPlansCleanWhenTheServiceIsDeclared(t *testing.T) {
	botsDir := t.TempDir()
	writeAgentLoopBot(t, botsDir, "researcher", `  services:
    - id: gdrive
      provider: google
`, `      tools:
        - name: list_files
          service: gdrive
          op: files.list
`, 5)
	res := planFallback(t, botsDir, `  bots:
    - id: r
      use: researcher@0.1.0
`)
	if errs := fallbackErrs(res); len(errs) > 0 {
		t.Fatalf("a service tool naming a declared service was rejected: %v", errs)
	}
}

func TestAgentLoopServiceToolNamingAnUndeclaredServiceIsRejected(t *testing.T) {
	botsDir := t.TempDir()
	writeAgentLoopBot(t, botsDir, "researcher", "", `      tools:
        - name: list_files
          service: gdrive
          op: files.list
`, 5)
	res := planFallback(t, botsDir, `  bots:
    - id: r
      use: researcher@0.1.0
`)
	if !containsSubstr(fallbackErrs(res), `does not declare`) {
		t.Errorf("expected a rejection naming the undeclared service, got %v", fallbackErrs(res))
	}
}

func TestAgentLoopToolWithBothServiceAndBuiltinIsRejected(t *testing.T) {
	botsDir := t.TempDir()
	writeAgentLoopBot(t, botsDir, "researcher", `  services:
    - id: gdrive
      provider: google
`, `      tools:
        - name: confused
          service: gdrive
          op: files.list
          builtin: web.fetch
`, 5)
	res := planFallback(t, botsDir, `  bots:
    - id: r
      use: researcher@0.1.0
`)
	if !containsSubstr(fallbackErrs(res), "both a service and a builtin") {
		t.Errorf("expected a rejection, got %v", fallbackErrs(res))
	}
}

func TestAgentLoopDuplicateToolNamesAreRejected(t *testing.T) {
	botsDir := t.TempDir()
	writeAgentLoopBot(t, botsDir, "researcher", "", `      tools:
        - name: fetch
          builtin: web.fetch
        - name: fetch
          builtin: memory.get
`, 5)
	res := planFallback(t, botsDir, `  bots:
    - id: r
      use: researcher@0.1.0
`)
	if !containsSubstr(fallbackErrs(res), "more than once") {
		t.Errorf("expected a rejection naming the duplicate, got %v", fallbackErrs(res))
	}
}
