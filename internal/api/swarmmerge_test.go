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
		nil, "", // leave the trigger alone
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
		nil, "",
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
		nil, "",
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

// retry_backoff, when, fallback, loop and swarm all reached schema.BotRef
// before builderBotRef caught up to them (v3 Phase 8) — the same class of
// loss on_error and join were once one line from, on the same wholesale
// bot-rebuild this file exists to guard. A swarm using one of these,
// opened in the builder and saved without ever touching it, must come
// back out with the field intact.
func TestMergePreservesTheNewerPerBotFields(t *testing.T) {
	existing := []byte(`apiVersion: nanobots.dev/v1
kind: Nanoswarm
metadata:
  name: s
  description: d
spec:
  bots:
    - id: poller
      use: watch-the-competition@0.1.0
`)
	out, err := mergeIntoExistingSwarm(existing, "s", "d",
		[]builderBotRef{
			{
				ID: "poller", Use: "watch-the-competition@0.1.0",
				RetryBackoff: "5s",
				When:         "{{inputs.amount}} > 500",
				Fallback:     "fixture-fetch@0.1.0",
				Loop:         &schema.Loop{Max: 20, Until: "{{outputs.done}}"},
			},
			{ID: "refund", Swarm: "refund-flow.yaml"},
		},
		nil, nil, "")
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	got := string(out)
	for _, want := range []string{
		`retry_backoff: 5s`,
		`when: '{{inputs.amount}} > 500'`,
		`fallback: fixture-fetch@0.1.0`,
		`max: 20`,
		`until: '{{outputs.done}}'`,
		`swarm: refund-flow.yaml`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("save dropped %q\n--- result ---\n%s", want, got)
		}
	}

	// And it has to still parse as a real swarm, not just look right.
	path := filepath.Join(t.TempDir(), "merged.yaml")
	if werr := os.WriteFile(path, out, 0o644); werr != nil {
		t.Fatal(werr)
	}
	sw, err := schema.LoadNanoswarm(path)
	if err != nil {
		t.Fatalf("merged output no longer parses as a swarm: %v\n%s", err, out)
	}
	poller := sw.Spec.Bots[0]
	if poller.RetryBackoff != "5s" || poller.When != "{{inputs.amount}} > 500" ||
		poller.Fallback != "fixture-fetch@0.1.0" {
		t.Errorf("poller = %+v, want the fields preserved", poller)
	}
	if poller.Loop == nil || poller.Loop.Max != 20 || poller.Loop.Until != "{{outputs.done}}" {
		t.Errorf("poller.Loop = %+v, want max 20 until {{outputs.done}}", poller.Loop)
	}
	if sw.Spec.Bots[1].Swarm != "refund-flow.yaml" {
		t.Errorf("refund.Swarm = %q, want refund-flow.yaml", sw.Spec.Bots[1].Swarm)
	}
}

func TestMergeRejectsGarbage(t *testing.T) {
	if _, err := mergeIntoExistingSwarm([]byte("\x00not yaml: ["), "n", "d", nil, nil, nil, ""); err == nil {
		t.Error("expected an error rather than a silently mangled file")
	}
}

// Pressing Save must not move a swarm to a different timezone.
//
// This file exists because "Save changes" once destroyed a swarm's cron
// trigger and its guardrails. It was doing the same thing to the timezone:
// the builder has no control to set one, so it always sent "", and the
// merge read that as "clear it" and deleted the line. The scheduler reads a
// missing timezone as UTC, so saving daily-inbox-recap moved its 7am
// Chicago run to 7am UTC — 2am Chicago — while the card still said 7:00 AM.
func TestSavingASwarmKeepsItsTimezone(t *testing.T) {
	existing := []byte(`apiVersion: nanobots.dev/v1
kind: Nanoswarm
metadata:
  name: daily-inbox-recap
  description: d
spec:
  bots: []
  trigger:
    type: cron
    expr: "0 7 * * 1-5"
    timezone: America/Chicago
`)
	sched := "0 7 * * 1-5"
	out, err := mergeIntoExistingSwarm(existing, "daily-inbox-recap", "d",
		[]builderBotRef{{ID: "recap", Use: "recap@0.1.0"}}, nil, &sched, "")
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)
	if !strings.Contains(got, "timezone: America/Chicago") {
		t.Errorf("the timezone was dropped on save:\n%s", got)
	}
}

// An explicit timezone still replaces the old one — this preserves, it does
// not freeze.
func TestAnExplicitTimezoneStillReplacesTheOldOne(t *testing.T) {
	existing := []byte(`apiVersion: nanobots.dev/v1
kind: Nanoswarm
metadata:
  name: s
  description: d
spec:
  bots: []
  trigger:
    type: cron
    expr: "0 7 * * *"
    timezone: America/Chicago
`)
	sched := "0 7 * * *"
	out, err := mergeIntoExistingSwarm(existing, "s", "d",
		[]builderBotRef{{ID: "b", Use: "b@0.1.0"}}, nil, &sched, "Asia/Tokyo")
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)
	if !strings.Contains(got, "timezone: Asia/Tokyo") || strings.Contains(got, "America/Chicago") {
		t.Errorf("explicit timezone did not win:\n%s", got)
	}
}

// A swarm with no timezone at all gets this machine's, so it means what the
// person setting it meant — same as a brand-new swarm from triggerFor.
func TestASwarmWithNoTimezoneGetsThisMachines(t *testing.T) {
	existing := []byte(`apiVersion: nanobots.dev/v1
kind: Nanoswarm
metadata:
  name: s
  description: d
spec:
  bots: []
  trigger:
    type: cron
    expr: "0 7 * * *"
`)
	sched := "0 7 * * *"
	out, err := mergeIntoExistingSwarm(existing, "s", "d",
		[]builderBotRef{{ID: "b", Use: "b@0.1.0"}}, nil, &sched, "")
	if err != nil {
		t.Fatal(err)
	}
	if local := localTimezoneName(); local != "" {
		if !strings.Contains(string(out), "timezone: "+local) {
			t.Errorf("expected this machine's timezone %q:\n%s", local, string(out))
		}
	}
}

// Switching a swarm to manual still clears the schedule and its timezone —
// preserving a timezone on a trigger that no longer has a schedule would
// leave a field describing nothing.
func TestGoingManualStillClearsTheSchedule(t *testing.T) {
	existing := []byte(`apiVersion: nanobots.dev/v1
kind: Nanoswarm
metadata:
  name: s
  description: d
spec:
  bots: []
  trigger:
    type: cron
    expr: "0 7 * * *"
    timezone: America/Chicago
`)
	manual := ""
	out, err := mergeIntoExistingSwarm(existing, "s", "d",
		[]builderBotRef{{ID: "b", Use: "b@0.1.0"}}, nil, &manual, "")
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)
	if strings.Contains(got, "timezone:") || strings.Contains(got, "expr:") {
		t.Errorf("manual swarm kept schedule fields:\n%s", got)
	}
	if !strings.Contains(got, "type: manual") {
		t.Errorf("not switched to manual:\n%s", got)
	}
}

// A live audit found this one: saving get-paid.yaml through the builder
// with no other change (only a bot added) glued its header comment
// straight onto metadata's last line and ran every section from defaults
// through deploy together with no separation at all — a save that edited
// nothing about the swarm's own formatting still visibly wrecked it,
// because neither yaml.v3 write path (mergeIntoExistingSwarm's node tree,
// or marshalSwarmYAML's plain struct marshal) tracks blank lines.
func TestRestoreSectionSpacingMatchesTheRealCatalogFileAfterAnEdit(t *testing.T) {
	root := repoRoot(t)
	existing, err := os.ReadFile(filepath.Join(root, "examples", "swarms", "get-paid.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	out, err := mergeIntoExistingSwarm(existing,
		"get-paid",
		"Every Monday, find every overdue invoice, send each reminder once approved, and post one summary of what went out.",
		[]builderBotRef{
			{ID: "chaser", Use: "invoice-chaser@0.1.0"},
			{ID: "sender", Use: "email-send-approved@0.1.0"},
			{ID: "notifier", Use: "notify@0.1.0", OnError: "continue"},
			{ID: "newbot", Use: "competitor-watch@0.1.0"}, // the actual edit
		},
		[]builderSnap{{From: "sender.acted_on", To: "notifier.message", Join: "lines"}},
		nil, "",
	)
	if err != nil {
		t.Fatal(err)
	}
	got := string(restoreSectionSpacing(out))

	// The header comment stays separated from metadata, not glued to it —
	// and separated from spec: on the other side too. The second half of
	// this was the actual live bug: a first version of this fix restored
	// only the blank line before the comment block, not the one after it,
	// so spec: still ran straight into the last comment line with no gap.
	if !strings.Contains(got, "owner: me@example.com\n\n# Catalog's S10") {
		t.Errorf("blank line before the header comment is missing:\n%s", got)
	}
	if !strings.Contains(got, "(docs/fan-out.md).\n\nspec:") {
		t.Errorf("blank line between the header comment and spec: is missing:\n%s", got)
	}
	// Every one of spec's own sections gets its blank line back, except
	// the first (defaults, which the real file also runs straight into
	// spec: with no gap).
	for _, want := range []string{
		"daily_budget_usd: 5\n      approval_required_for: [email.send]\n    resources: {preset: small}\n\n  vars:",
		"notify_channel: \"slack:#finance\"\n\n  trigger:",
		"timezone: America/Chicago\n\n  bots:",
		"use: competitor-watch@0.1.0\n\n  snaps:",
		"join: lines\n\n  deploy:",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing expected spacing %q\n--- result ---\n%s", want, got)
		}
	}
	if strings.Contains(got, "spec:\n\n  defaults:") {
		t.Error("a blank line was added before defaults:, which the real file never has")
	}
}

func TestRestoreSectionSpacingIsIdempotent(t *testing.T) {
	once := restoreSectionSpacing([]byte(existingSwarm))
	twice := restoreSectionSpacing(once)
	if string(once) != string(twice) {
		t.Errorf("running it twice kept adding blank lines:\n--- once ---\n%s\n--- twice ---\n%s", once, twice)
	}
}

func TestRestoreSectionSpacingLeavesAFileWithNoSpecAlone(t *testing.T) {
	malformed := []byte("apiVersion: nanobots.dev/v1\nkind: Nanoswarm\n")
	got := restoreSectionSpacing(malformed)
	if string(got) != string(malformed) {
		t.Errorf("a document with no spec: was changed:\n%s", got)
	}
}
