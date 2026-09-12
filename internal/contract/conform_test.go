package contract

import (
	"github.com/redbotster/nanobots/internal/schema"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// this file: internal/contract/conform_test.go
	return filepath.Join(filepath.Dir(file), "..", "..")
}

// allBotIDs discovers every bot under bots/<id>/nanobot.yaml, so this test
// covers new bricks automatically instead of relying on someone remembering
// to add them to a hardcoded list.
func allBotIDs(t *testing.T, root string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(root, "bots"))
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(root, "bots", e.Name(), "nanobot.yaml")); err == nil {
			ids = append(ids, e.Name())
		}
	}
	if len(ids) == 0 {
		t.Fatal("no bots found under bots/ — did the test find the wrong repo root?")
	}
	return ids
}

func TestRunConformanceOnLaunchBots(t *testing.T) {
	root := repoRoot(t)
	bots := allBotIDs(t, root)
	for _, id := range bots {
		id := id
		t.Run(id, func(t *testing.T) {
			report, err := RunConformance(filepath.Join(root, "bots", id), "")
			if err != nil {
				t.Fatalf("RunConformance: %v", err)
			}
			if !report.OK() {
				t.Fatalf("bot %s failed conformance:\n%s", id, report.String())
			}
			if len(report.Outputs) == 0 {
				t.Errorf("bot %s: expected some outputs, got none", id)
			}
		})
	}
}

// TestRunConformanceSeedsRealBlobContentForFileInputs proves fixtures/<port>.content
// actually reaches an ai.generate prompt as real text, not a blob reference —
// what bots/repurposer's conformance test depends on.
func TestRunConformanceSeedsRealBlobContentForFileInputs(t *testing.T) {
	dir := t.TempDir()
	nanobotYAML := `
apiVersion: nanobots.dev/v1alpha1
kind: Nanobot
metadata:
  name: content-echo-test
  version: 0.1.0
spec:
  harness: { type: bare }
  ports:
    inputs:
      - name: source
        type: file
        required: true
    outputs:
      - name: echoed
        type: json
  steps:
    - name: echo
      type: ai.generate
      prompt_file: ./prompt.md
      inputs: { source: "{{inputs.source}}" }
      output: echoed
`
	if err := os.WriteFile(filepath.Join(dir, "nanobot.yaml"), []byte(nanobotYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "prompt.md"), []byte("SOURCE:{{source}}"), 0o644); err != nil {
		t.Fatal(err)
	}
	fixturesDir := filepath.Join(dir, "fixtures")
	if err := os.MkdirAll(fixturesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixturesDir, "inputs.json"), []byte(`{"source": {"uri": "nbf://placeholder", "mime": "text/plain"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixturesDir, "source.content"), []byte("the real transcript text"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixturesDir, "ai.generate.json"), []byte(`{"ok": true}`), 0o644); err != nil {
		t.Fatal(err)
	}

	report, err := RunConformance(dir, "")
	if err != nil {
		t.Fatalf("RunConformance: %v", err)
	}
	if !report.OK() {
		t.Fatalf("conformance failed:\n%s", report.String())
	}
}

func TestRunConformanceMissingInputsFixtureErrors(t *testing.T) {
	root := repoRoot(t)
	_, err := RunConformance(filepath.Join(root, "bots", "email-drive-file"), filepath.Join(root, "bots"))
	if err == nil {
		t.Fatal("expected an error when fixtures/inputs.json is missing from the given dir")
	}
}

// A declared input port that nothing reads is a promise the bot doesn't
// keep: a swarm can snap real data into it, the planner will type-check
// that snap, and the value is then silently discarded. The AI composer sees
// these ports in the catalog too, so it can wire one in good faith.
//
// Seven of them existed when this test was written — two voice_sample
// ports, past_posts, kb, two template ports and a schedule. Four were
// wired up; three couldn't be honoured at all and were removed.
func TestNoBotDeclaresAnInputNothingReads(t *testing.T) {
	root := repoRoot(t)
	botsDir := filepath.Join(root, "bots")
	entries, err := os.ReadDir(botsDir)
	if err != nil {
		t.Fatal(err)
	}

	var dead []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(botsDir, e.Name())
		nb, err := schema.LoadNanobot(filepath.Join(dir, "nanobot.yaml"))
		if err != nil {
			continue
		}

		// Everything a step could read the port through: a template
		// reference anywhere in the spec, or {{name}} in a prompt file.
		raw, err := os.ReadFile(filepath.Join(dir, "nanobot.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		haystack := string(raw)
		for _, s := range nb.Spec.Steps {
			if s.PromptFile == "" {
				continue
			}
			body, err := os.ReadFile(filepath.Join(dir, filepath.Clean(s.PromptFile)))
			if err == nil {
				haystack += string(body)
			}
		}

		for _, p := range nb.Spec.Ports.Inputs {
			if strings.Contains(haystack, "inputs."+p.Name) ||
				strings.Contains(haystack, "{{"+p.Name+"}}") {
				continue
			}
			dead = append(dead, e.Name()+"."+p.Name)
		}
	}

	if len(dead) > 0 {
		t.Errorf("these input ports are declared but nothing reads them, so a snap into one is silently discarded:\n  %s\n"+
			"Either wire the port into a step or a prompt, or remove it from the port list.",
			strings.Join(dead, "\n  "))
	}
}
