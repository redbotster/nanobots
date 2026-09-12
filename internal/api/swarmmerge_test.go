package api

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/redbotster/nanobots/internal/schema"
)

// Trimmed from the real examples/swarms/bookkeeping-assistant.yaml — every
// part of it is something the builder does not model and therefore used to
// destroy on save.
const existingSwarm = `apiVersion: nanobots.dev/v1alpha1
kind: Nanoswarm
metadata:
  name: bookkeeping-assistant
  description: File today's receipts and produce a spend report.
  owner: me@example.com

# This header explains a real simplification the author made, and is the
# kind of thing that must survive an unrelated edit.
spec:
  defaults:
    model: { provider: anthropic, name: claude-sonnet-4-6 }
    guardrails:
      pii: redact
      injection_threshold: 0.7
      daily_budget_usd: 5

  vars:
    notify_channel: "slack:#finance"

  trigger:
    type: cron
    expr: "0 15 * * 5"
    timezone: America/Chicago

  bots:
    - id: receipts
      use: receipt-filer@0.1.0
    - id: reporter
      use: sheet-reporter@0.1.0

  snaps:
    - from: receipts.receipts
      to: reporter.sheet

  deploy:
    target: local
`

func TestMergePreservesEverythingTheBuilderDoesNotModel(t *testing.T) {
	out, err := mergeIntoExistingSwarm(
		[]byte(existingSwarm),
		"bookkeeping-assistant",
		"File today's receipts and produce a spend report.",
		[]builderBotRef{
			{ID: "receipts", Use: "receipt-filer@0.1.0"},
			{ID: "reporter", Use: "sheet-reporter@0.1.0"},
			{ID: "pdf", Use: "render-pdf@0.1.0"}, // the actual edit
		},
		[]builderSnap{{From: "receipts.receipts", To: "reporter.sheet"}},
	)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	got := string(out)

	// The whole point: a save that changes the bot list must not silently
	// unschedule the swarm or drop its guardrails.
	for _, want := range []string{
		"type: cron",
		`"0 15 * * 5"`,
		"America/Chicago",
		"notify_channel",
		"pii: redact",
		"injection_threshold: 0.7",
		"daily_budget_usd: 5",
		"owner: me@example.com",
		"target: local",
		"claude-sonnet-4-6",
		"# This header explains a real simplification",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("save destroyed %q\n--- result ---\n%s", want, got)
		}
	}

	// And the edit itself actually landed.
	if !strings.Contains(got, "render-pdf@0.1.0") {
		t.Errorf("the newly added bot is missing\n--- result ---\n%s", got)
	}
	if strings.Contains(got, "type: manual") {
		t.Error("the cron trigger was replaced with manual — the exact bug this fixes")
	}
}

func TestMergeAppliesRenamesAndRemovals(t *testing.T) {
	out, err := mergeIntoExistingSwarm(
		[]byte(existingSwarm),
		"renamed-swarm",
		"A new description.",
		[]builderBotRef{{ID: "only", Use: "notify@0.1.0"}},
		nil, // every snap removed
	)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	got := string(out)

	if !strings.Contains(got, "name: renamed-swarm") || !strings.Contains(got, "A new description.") {
		t.Errorf("rename did not apply\n%s", got)
	}
	if strings.Contains(got, "receipt-filer") || strings.Contains(got, "sheet-reporter") {
		t.Errorf("removed bots are still present\n%s", got)
	}
	// Removing every snap must remove the key, not leave an empty list that
	// re-parses as a swarm with a stray `snaps: []`.
	if strings.Contains(got, "snaps:") {
		t.Errorf("snaps key survived with no snaps\n%s", got)
	}
	// Still must not have eaten the trigger.
	if !strings.Contains(got, "type: cron") {
		t.Errorf("trigger lost during a rename\n%s", got)
	}
}

// A merged file has to still load as a real swarm, not just look right.
func TestMergedSwarmStillParses(t *testing.T) {
	out, err := mergeIntoExistingSwarm(
		[]byte(existingSwarm), "bookkeeping-assistant", "d",
		[]builderBotRef{{ID: "a", Use: "notify@0.1.0"}},
		[]builderSnap{},
	)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	// Round-trip through the real loader, from a real file, so this proves
	// what the daemon would actually read back.
	path := filepath.Join(t.TempDir(), "merged.yaml")
	if werr := os.WriteFile(path, out, 0o644); werr != nil {
		t.Fatal(werr)
	}
	sw, err := schema.LoadNanoswarm(path)
	if err != nil {
		t.Fatalf("merged output no longer parses as a swarm: %v\n%s", err, out)
	}
	if sw.Spec.Trigger.Type != "cron" || sw.Spec.Trigger.Expr != "0 15 * * 5" {
		t.Errorf("trigger = %+v, want the original cron", sw.Spec.Trigger)
	}
	if sw.Spec.Vars["notify_channel"] != "slack:#finance" {
		t.Errorf("vars = %+v, want notify_channel preserved", sw.Spec.Vars)
	}
}

func TestMergeRejectsGarbage(t *testing.T) {
	if _, err := mergeIntoExistingSwarm([]byte("\x00not yaml: ["), "n", "d", nil, nil); err == nil {
		t.Error("expected an error rather than a silently mangled file")
	}
}
