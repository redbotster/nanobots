package contract

import (
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

func TestRunConformanceOnLaunchBots(t *testing.T) {
	root := repoRoot(t)
	bots := []string{"email-drive-file", "recap-emails-to-pdf"}
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
