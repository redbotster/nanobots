package runner

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/redbotster/nanobots/internal/planner"
	"github.com/redbotster/nanobots/internal/schema"
	"github.com/redbotster/nanobots/internal/step"
)

func TestResolveInputsPrefersExplicitThenSnapThenDefault(t *testing.T) {
	rs := &planner.ResolvedSwarm{
		Swarm: &schema.Nanoswarm{
			Spec: schema.NanoswarmSpec{
				Vars: map[string]any{"notify_to": "me@example.com"},
				Snaps: []schema.Snap{
					{From: "upstream.file_id", To: "mailer.file_id"},
				},
			},
		},
	}
	rb := &planner.ResolvedBot{
		Ref: schema.BotRef{Inputs: map[string]any{"to": "{{vars.notify_to}}"}},
		Nanobot: &schema.Nanobot{
			Spec: schema.NanobotSpec{
				Ports: schema.Ports{Inputs: []schema.InputPort{
					{Name: "to", Type: "string"},
					{Name: "file_id", Type: "string"},
					{Name: "subject", Type: "string", Default: "hello"},
				}},
			},
		},
	}
	run := NewRun("test-swarm")
	run.SetBotOutputs("upstream", map[string]any{"file_id": "f-123"})

	o := &Orchestrator{}
	inputs, err := o.resolveInputs(run, rs, "mailer", rb)
	if err != nil {
		t.Fatalf("resolveInputs: %v", err)
	}
	if inputs["to"] != "me@example.com" {
		t.Errorf("to = %v, want me@example.com (explicit + template)", inputs["to"])
	}
	if inputs["file_id"] != "f-123" {
		t.Errorf("file_id = %v, want f-123 (from snap)", inputs["file_id"])
	}
	if inputs["subject"] != "hello" {
		t.Errorf("subject = %v, want hello (default)", inputs["subject"])
	}
}

func TestResolveInputsMissingRequiredErrors(t *testing.T) {
	rs := &planner.ResolvedSwarm{Swarm: &schema.Nanoswarm{}}
	rb := &planner.ResolvedBot{
		Nanobot: &schema.Nanobot{Spec: schema.NanobotSpec{
			Ports: schema.Ports{Inputs: []schema.InputPort{{Name: "must_have", Type: "string", Required: true}}},
		}},
	}
	o := &Orchestrator{}
	_, err := o.resolveInputs(NewRun("s"), rs, "bot", rb)
	if err == nil {
		t.Fatal("expected an error for a missing required input with no default or snap")
	}
}

func TestResolveSnapValueDrillsIntoField(t *testing.T) {
	rs := &planner.ResolvedSwarm{}
	run := NewRun("s")
	run.SetBotOutputs("recap", map[string]any{
		"recap_json": map[string]any{"headline": "hello world"},
	})
	o := &Orchestrator{}
	val, err := o.resolveSnapValue(run, schema.Snap{From: "recap.recap_json.headline", To: "mailer.body_intro"})
	if err != nil {
		t.Fatalf("resolveSnapValue: %v", err)
	}
	if val != "hello world" {
		t.Errorf("val = %v, want %q", val, "hello world")
	}
	_ = rs
}

func TestCollectOutputsScalarAndFile(t *testing.T) {
	outDir := t.TempDir()
	os.WriteFile(filepath.Join(outDir, "message_id.json"), []byte(`"abc123"`), 0o644)
	os.WriteFile(filepath.Join(outDir, "recap_pdf"), []byte("%PDF-fake"), 0o644)
	os.WriteFile(filepath.Join(outDir, "recap_pdf.mime"), []byte("application/pdf"), 0o644)

	nb := &schema.Nanobot{Spec: schema.NanobotSpec{Ports: schema.Ports{Outputs: []schema.OutputPort{
		{Name: "message_id", Type: "string"},
		{Name: "recap_pdf", Type: "file"},
	}}}}
	blobs, err := step.NewFSBlobStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	outputs, err := collectOutputs(nb, blobs, outDir)
	if err != nil {
		t.Fatalf("collectOutputs: %v", err)
	}
	if outputs["message_id"] != "abc123" {
		t.Errorf("message_id = %v", outputs["message_id"])
	}
	fv, ok := outputs["recap_pdf"].(map[string]any)
	if !ok || fv["mime"] != "application/pdf" {
		t.Errorf("recap_pdf = %#v", outputs["recap_pdf"])
	}
	uri, _ := fv["uri"].(string)
	data, err := blobs.Read(uri)
	if err != nil || string(data) != "%PDF-fake" {
		t.Errorf("blob read back = %q, %v", data, err)
	}
}

func TestCollectOutputsMissingPortErrors(t *testing.T) {
	nb := &schema.Nanobot{Spec: schema.NanobotSpec{Ports: schema.Ports{Outputs: []schema.OutputPort{
		{Name: "never_written", Type: "string"},
	}}}}
	blobs, _ := step.NewFSBlobStore(t.TempDir())
	if _, err := collectOutputs(nb, blobs, t.TempDir()); err == nil {
		t.Fatal("expected an error for a declared output port that was never written")
	}
}
