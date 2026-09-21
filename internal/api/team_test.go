package api

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/redbotster/nanobots/internal/schema"
)

// The tab was called Fleet, and its state file fleet.json. Renaming the tab
// must not throw away every instruction someone had written: the whole
// point of recording what a bot shipped with is that a change can be
// undone, and losing the record loses that too.
func TestTeamStoreReadsTheOldFleetFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "fleet.json"),
		[]byte(`{"inbox-triage":{"shipped":"the original wording"}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	s := &TeamStore{Path: filepath.Join(dir, "team.json")}
	got, err := s.load()
	if err != nil {
		t.Fatal(err)
	}
	rec, ok := got["inbox-triage"]
	if !ok {
		t.Fatalf("the old file was ignored: %+v", got)
	}
	if rec.Shipped == nil || *rec.Shipped != "the original wording" {
		t.Errorf("shipped = %v", rec.Shipped)
	}
}

// Once there is a team.json it wins outright — an old fleet.json left on
// disk must not resurrect anything.
func TestTheNewFileWinsOverTheOld(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "fleet.json"), []byte(`{"old":{"shipped":"stale"}}`), 0o600)
	_ = os.WriteFile(filepath.Join(dir, "team.json"), []byte(`{"new":{"shipped":"current"}}`), 0o600)

	got, err := (&TeamStore{Path: filepath.Join(dir, "team.json")}).load()
	if err != nil {
		t.Fatal(err)
	}
	if _, stale := got["old"]; stale {
		t.Error("the superseded file was merged in")
	}
	if _, ok := got["new"]; !ok {
		t.Error("the current file was not read")
	}
}

// A bot guardrails-tuned first used to have its instructions' real shipped
// value silently skipped the first time those were later touched too —
// RecordTuned's "only the first call records" check saw a record already
// existed (from the guardrails tuning) and returned early, never setting
// Shipped at all. "Put back" for instructions would then have had nothing
// to put back to.
func TestRecordTunedStillRecordsInstructionsWhenOnlyGuardrailsWereTunedFirst(t *testing.T) {
	s := &TeamStore{Path: filepath.Join(t.TempDir(), "team.json")}
	if err := s.RecordGuardrailsTuned("inbox-triage", schema.Guardrails{PII: "redact"}); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordTuned("inbox-triage", "the real shipped instructions"); err != nil {
		t.Fatal(err)
	}
	shipped, known := s.Shipped("inbox-triage")
	if !known || shipped != "the real shipped instructions" {
		t.Errorf("Shipped = %q, %v, want the real value recorded", shipped, known)
	}
}

// The two are independent: putting instructions back must not throw away
// a guardrails customisation already on record, and vice versa.
func TestForgettingOneAxisLeavesTheOthersRecordIntact(t *testing.T) {
	s := &TeamStore{Path: filepath.Join(t.TempDir(), "team.json")}
	if err := s.RecordTuned("inbox-triage", "shipped text"); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordGuardrailsTuned("inbox-triage", schema.Guardrails{PII: "redact"}); err != nil {
		t.Fatal(err)
	}

	if err := s.ForgetInstructions("inbox-triage"); err != nil {
		t.Fatal(err)
	}
	if _, known := s.Shipped("inbox-triage"); known {
		t.Error("instructions should no longer be recorded as tuned")
	}
	if g, known := s.ShippedGuardrails("inbox-triage"); !known || g.PII != "redact" {
		t.Errorf("guardrails record was lost: %+v, known=%v", g, known)
	}

	if err := s.ForgetGuardrails("inbox-triage"); err != nil {
		t.Fatal(err)
	}
	if _, known := s.ShippedGuardrails("inbox-triage"); known {
		t.Error("guardrails should no longer be recorded as tuned")
	}
}

// Once both axes are forgotten, the bot must actually leave the team, not
// linger as an empty record forever.
func TestABotWithNothingTunedLeavesTheTeamEntirely(t *testing.T) {
	path := filepath.Join(t.TempDir(), "team.json")
	s := &TeamStore{Path: path}
	if err := s.RecordTuned("inbox-triage", "shipped text"); err != nil {
		t.Fatal(err)
	}
	if err := s.ForgetInstructions("inbox-triage"); err != nil {
		t.Fatal(err)
	}
	m, err := s.load()
	if err != nil {
		t.Fatal(err)
	}
	if _, present := m["inbox-triage"]; present {
		t.Errorf("expected the bot to be dropped from the team entirely, got %+v", m)
	}
}

// RecordGuardrailsTuned keeps the original shipped guardrails, the same
// "only the first call records" discipline RecordTuned already has.
func TestRecordGuardrailsTunedOnlyRecordsTheFirstShippedValue(t *testing.T) {
	s := &TeamStore{Path: filepath.Join(t.TempDir(), "team.json")}
	if err := s.RecordGuardrailsTuned("inbox-triage", schema.Guardrails{PII: "redact"}); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordGuardrailsTuned("inbox-triage", schema.Guardrails{PII: "block"}); err != nil {
		t.Fatal(err)
	}
	g, known := s.ShippedGuardrails("inbox-triage")
	if !known || g.PII != "redact" {
		t.Errorf("ShippedGuardrails = %+v, %v, want the original (redact), not the second call's value", g, known)
	}
}
