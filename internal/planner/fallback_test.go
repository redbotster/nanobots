package planner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFallbackBot writes a minimal, self-contained nanobot.yaml into a temp
// bots directory — the fallback checks need bots whose output ports we
// control exactly, which the real catalog doesn't happen to offer in pairs.
func writeFallbackBot(t *testing.T, botsDir, name, ports string) {
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
    type: bare
  ports:
` + ports + `
  steps:
    - name: noop
      type: log
      value: probe
`
	if err := os.WriteFile(filepath.Join(dir, "nanobot.yaml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

const fallbackSwarmHeader = `apiVersion: nanobots.dev/v1alpha1
kind: Nanoswarm
metadata:
  name: probe
  description: probe
spec:
  snaps: []
`

func planFallback(t *testing.T, botsDir, botsBlock string) *PlanResult {
	t.Helper()
	path := filepath.Join(t.TempDir(), "probe.yaml")
	if err := os.WriteFile(path, []byte(fallbackSwarmHeader+botsBlock), 0o600); err != nil {
		t.Fatal(err)
	}
	res, err := Plan(path, botsDir)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func fallbackErrs(res *PlanResult) []string {
	var out []string
	for _, e := range res.Invalid {
		out = append(out, e.Error())
	}
	return out
}

func containsSubstr(hay []string, needle string) bool {
	for _, h := range hay {
		if strings.Contains(h, needle) {
			return true
		}
	}
	return false
}

// A fallback whose output ports match the primary bot's exactly is what
// downstream snaps type-checked against, so it plans clean.
func TestAFallbackWithMatchingOutputsPlansClean(t *testing.T) {
	botsDir := t.TempDir()
	writeFallbackBot(t, botsDir, "live-fetch", `    inputs:
      - name: url
        type: string
        required: true
    outputs:
      - name: body
        type: string`)
	writeFallbackBot(t, botsDir, "fixture-fetch", `    inputs:
      - name: url
        type: string
        required: true
    outputs:
      - name: body
        type: string`)

	res := planFallback(t, botsDir, `  bots:
    - id: fetch
      use: live-fetch@0.1.0
      fallback: fixture-fetch@0.1.0
      inputs: { url: "https://example.com" }
`)
	if errs := fallbackErrs(res); len(errs) > 0 {
		t.Fatalf("matching fallback rejected: %v", errs)
	}
}

func TestAFallbackWithADifferentNumberOfOutputsIsRejected(t *testing.T) {
	botsDir := t.TempDir()
	writeFallbackBot(t, botsDir, "live-fetch", `    outputs:
      - name: body
        type: string
      - name: status
        type: string`)
	writeFallbackBot(t, botsDir, "fixture-fetch", `    outputs:
      - name: body
        type: string`)

	res := planFallback(t, botsDir, `  bots:
    - id: fetch
      use: live-fetch@0.1.0
      fallback: fixture-fetch@0.1.0
`)
	if !containsSubstr(fallbackErrs(res), "they must match exactly") {
		t.Fatalf("expected an output-count mismatch error, got %v", fallbackErrs(res))
	}
}

func TestAFallbackMissingAnOutputPortByNameIsRejected(t *testing.T) {
	botsDir := t.TempDir()
	writeFallbackBot(t, botsDir, "live-fetch", `    outputs:
      - name: body
        type: string`)
	writeFallbackBot(t, botsDir, "fixture-fetch", `    outputs:
      - name: text
        type: string`)

	res := planFallback(t, botsDir, `  bots:
    - id: fetch
      use: live-fetch@0.1.0
      fallback: fixture-fetch@0.1.0
`)
	if !containsSubstr(fallbackErrs(res), `no output port named "body"`) {
		t.Fatalf("expected a missing-output-port error, got %v", fallbackErrs(res))
	}
}

func TestAFallbackWithAMismatchedOutputTypeIsRejected(t *testing.T) {
	botsDir := t.TempDir()
	writeFallbackBot(t, botsDir, "live-fetch", `    outputs:
      - name: body
        type: string`)
	writeFallbackBot(t, botsDir, "fixture-fetch", `    outputs:
      - name: body
        type: json`)

	res := planFallback(t, botsDir, `  bots:
    - id: fetch
      use: live-fetch@0.1.0
      fallback: fixture-fetch@0.1.0
`)
	if !containsSubstr(fallbackErrs(res), `not "string" like "live-fetch@0.1.0"'s`) {
		t.Fatalf("expected a type-mismatch error, got %v", fallbackErrs(res))
	}
}

// The fallback runs with this bot instance's own resolved inputs — nothing
// is re-wired for it — so a fallback that requires a port the primary
// doesn't have can never actually run.
func TestAFallbackRequiringAnUnavailableInputIsRejected(t *testing.T) {
	botsDir := t.TempDir()
	writeFallbackBot(t, botsDir, "live-fetch", `    outputs:
      - name: body
        type: string`)
	writeFallbackBot(t, botsDir, "fixture-fetch", `    inputs:
      - name: seed
        type: string
        required: true
    outputs:
      - name: body
        type: string`)

	res := planFallback(t, botsDir, `  bots:
    - id: fetch
      use: live-fetch@0.1.0
      fallback: fixture-fetch@0.1.0
`)
	if !containsSubstr(fallbackErrs(res), `requires input "seed"`) {
		t.Fatalf("expected an unavailable-input error, got %v", fallbackErrs(res))
	}
}

func TestABotCannotFallBackToItself(t *testing.T) {
	botsDir := t.TempDir()
	writeFallbackBot(t, botsDir, "live-fetch", `    outputs:
      - name: body
        type: string`)

	res := planFallback(t, botsDir, `  bots:
    - id: fetch
      use: live-fetch@0.1.0
      fallback: live-fetch@0.1.0
`)
	if !containsSubstr(fallbackErrs(res), "cannot fall back to itself") {
		t.Fatalf("expected a self-fallback error, got %v", fallbackErrs(res))
	}
}

func TestAFallbackThatDoesNotResolveIsRejected(t *testing.T) {
	botsDir := t.TempDir()
	writeFallbackBot(t, botsDir, "live-fetch", `    outputs:
      - name: body
        type: string`)

	// An unresolvable fallback fails Resolve itself, before CheckFallback
	// ever runs — Plan surfaces that as an error, not a PlanResult, so this
	// can't go through the planFallback helper (which fatals on that error).
	path := filepath.Join(t.TempDir(), "probe.yaml")
	body := fallbackSwarmHeader + `  bots:
    - id: fetch
      use: live-fetch@0.1.0
      fallback: no-such-bot@0.1.0
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Plan(path, botsDir); err == nil {
		t.Fatal("an unresolvable fallback planned OK")
	}
}
