package contract

import (
	"os"
	"path/filepath"
	"runtime"
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

func TestRunConformanceMissingInputsFixtureErrors(t *testing.T) {
	root := repoRoot(t)
	_, err := RunConformance(filepath.Join(root, "bots", "email-drive-file"), filepath.Join(root, "bots"))
	if err == nil {
		t.Fatal("expected an error when fixtures/inputs.json is missing from the given dir")
	}
}
