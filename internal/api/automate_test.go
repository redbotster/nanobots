package api

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// testServerForAutomate layers a scratch SwarmsDir onto
// testServerForCompose, so ComposeAndSaveAutomation's real write never
// touches this repo's own examples/swarms/.
func testServerForAutomate(t *testing.T, shroudResponse string) *Server {
	t.Helper()
	srv := testServerForCompose(t, shroudResponse)
	srv.SwarmsDir = t.TempDir()
	return srv
}

func TestComposeAndSaveAutomationSavesARealFile(t *testing.T) {
	modelResponse := `{
		"name": "Daily inbox recap",
		"description": "Recap my inbox and email me the link",
		"bots": [
			{"id": "recap", "use": "recap-emails-to-pdf@0.3.0"},
			{"id": "mailer", "use": "email-drive-file@1.1.0", "inputs": {"to": "me@example.com"}}
		],
		"snaps": [
			{"from": "recap.drive_file_id", "to": "mailer.file_id"}
		]
	}`
	srv := testServerForAutomate(t, modelResponse)
	result, err := srv.ComposeAndSaveAutomation(context.Background(), "recap my inbox every morning")
	if err != nil {
		t.Fatalf("ComposeAndSaveAutomation: %v", err)
	}
	if result.Gap != "" {
		t.Fatalf("expected no gap, got %q", result.Gap)
	}
	if result.Name != "Daily inbox recap" {
		t.Errorf("Name = %q", result.Name)
	}
	if !result.PlanOK {
		t.Errorf("expected PlanOK, got PlanError=%q", result.PlanError)
	}

	// The file must actually exist on disk — this is the whole point of
	// "auto-save, don't auto-run": a real, saved swarm the human can open,
	// not just a description of one.
	if _, err := os.Stat(filepath.Join(srv.SwarmsDir, filepath.Base(result.Path))); err != nil {
		t.Errorf("expected the swarm file to exist: %v", err)
	}
}

func TestComposeAndSaveAutomationSurfacesAGapWithoutSavingAnything(t *testing.T) {
	modelResponse := `{"gap": true, "missing_capability": "place a stock trade"}`
	srv := testServerForAutomate(t, modelResponse)
	result, err := srv.ComposeAndSaveAutomation(context.Background(), "trade stocks for me")
	if err != nil {
		t.Fatalf("ComposeAndSaveAutomation: %v", err)
	}
	if result.Gap != "place a stock trade" {
		t.Fatalf("Gap = %q", result.Gap)
	}
	entries, err := os.ReadDir(srv.SwarmsDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("expected nothing saved for a declared gap, found %v", entries)
	}
}

func TestComposeAndSaveAutomationSavesEvenWhenASnapDoesNotTypeCheck(t *testing.T) {
	// Mirrors handleSaveSwarm's own policy: a work in progress is saved
	// anyway, and the caller is told which part doesn't connect yet.
	modelResponse := `{
		"name": "Bad draft",
		"bots": [
			{"id": "recap", "use": "recap-emails-to-pdf@0.3.0"},
			{"id": "mailer", "use": "email-drive-file@1.1.0", "inputs": {"to": "me@example.com"}}
		],
		"snaps": [
			{"from": "recap.recap_json", "to": "mailer.file_id"}
		]
	}`
	srv := testServerForAutomate(t, modelResponse)
	result, err := srv.ComposeAndSaveAutomation(context.Background(), "anything")
	if err != nil {
		t.Fatalf("ComposeAndSaveAutomation: %v", err)
	}
	if result.PlanOK {
		t.Error("expected PlanOK to be false for a type-mismatched snap")
	}
	if result.PlanError == "" {
		t.Error("expected PlanError to name what doesn't connect")
	}
	if _, err := os.Stat(filepath.Join(srv.SwarmsDir, filepath.Base(result.Path))); err != nil {
		t.Errorf("expected the swarm file to be saved anyway: %v", err)
	}
}

func TestComposeAndSaveAutomationRejectsWhenNoModelIsConfigured(t *testing.T) {
	srv := testServer(t) // no OneClaw, no LLM
	srv.SwarmsDir = t.TempDir()
	if _, err := srv.ComposeAndSaveAutomation(context.Background(), "recap my inbox"); err == nil {
		t.Fatal("expected an error when no model is configured")
	}
}
