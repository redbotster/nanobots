package step

import (
	"encoding/base64"
	"fmt"
	"testing"

	"github.com/redbotster/nanobots/internal/schema"
)

// fakeDeps is a minimal, fully-controllable Deps for unit testing the
// interpreter's control flow without touching fixtures, Chrome, or a real
// blob store.
type fakeDeps struct {
	serviceResult any
	serviceErr    error
	aiResult      string
	aiErr         error
	approve       bool
	approvedBy    string
	memory        map[string]string
	notifyCalled  bool
	renderResult  []byte
	renderMime    string
	blobs         BlobStore
}

func (f *fakeDeps) ServiceCall(svc schema.Service, op string, params map[string]any) (any, error) {
	return f.serviceResult, f.serviceErr
}
func (f *fakeDeps) AIGenerate(prompt string, model schema.Model) (string, error) {
	return f.aiResult, f.aiErr
}
func (f *fakeDeps) Render(templatePath string, data any, to string) ([]byte, string, error) {
	return f.renderResult, f.renderMime, nil
}
func (f *fakeDeps) Now() string { return "2026-09-10T00:00:00Z" }
func (f *fakeDeps) MemoryGet(namespace, key string) (string, bool, error) {
	v, ok := f.memory[namespace+"/"+key]
	return v, ok, nil
}
func (f *fakeDeps) MemoryPut(namespace, key, value string) error {
	if f.memory == nil {
		f.memory = map[string]string{}
	}
	f.memory[namespace+"/"+key] = value
	return nil
}
func (f *fakeDeps) Approve(summary, riskTier string) (bool, string, error) {
	return f.approve, f.approvedBy, nil
}
func (f *fakeDeps) Notify(message, channel string) error { f.notifyCalled = true; return nil }
func (f *fakeDeps) Blobs() BlobStore {
	if f.blobs != nil {
		return f.blobs
	}
	return &FSBlobStore{}
}

func simpleBot(steps []schema.Step, outputs []schema.OutputPort) *schema.Nanobot {
	return &schema.Nanobot{
		Metadata: schema.Metadata{Name: "test-bot"},
		Spec: schema.NanobotSpec{
			Services: []schema.Service{{ID: "svc", Provider: "demo"}},
			Ports:    schema.Ports{Outputs: outputs},
			Steps:    steps,
		},
	}
}

func TestInterpretServiceCallBindsOutput(t *testing.T) {
	nb := simpleBot(
		[]schema.Step{{Name: "call", Type: "service.call", Service: "svc", Op: "op", Output: "result"}},
		[]schema.OutputPort{{Name: "result", Type: "string"}},
	)
	deps := &fakeDeps{serviceResult: "hello"}
	res, err := Interpret(nb, nil, nil, deps)
	if err != nil {
		t.Fatalf("Interpret: %v", err)
	}
	if res.Outputs["result"] != "hello" {
		t.Errorf("outputs[result] = %v, want hello", res.Outputs["result"])
	}
}

func TestInterpretMissingOutputPortErrors(t *testing.T) {
	nb := simpleBot(
		[]schema.Step{{Name: "call", Type: "service.call", Service: "svc", Op: "op"}}, // no `output:`
		[]schema.OutputPort{{Name: "result", Type: "string"}},
	)
	deps := &fakeDeps{serviceResult: "hello"}
	_, err := Interpret(nb, nil, nil, deps)
	if err == nil {
		t.Fatal("expected an error for a never-produced output port")
	}
}

func TestInterpretTypeMismatchErrors(t *testing.T) {
	nb := simpleBot(
		[]schema.Step{{Name: "call", Type: "service.call", Service: "svc", Op: "op", Output: "result"}},
		[]schema.OutputPort{{Name: "result", Type: "boolean"}},
	)
	deps := &fakeDeps{serviceResult: "not-a-bool"}
	_, err := Interpret(nb, nil, nil, deps)
	if err == nil {
		t.Fatal("expected a type-mismatch error")
	}
}

func TestInterpretApproveRejectionStopsExecution(t *testing.T) {
	nb := simpleBot(
		[]schema.Step{
			{Name: "gate", Type: "approve", Summary: "send it?"},
			{Name: "call", Type: "service.call", Service: "svc", Op: "op", Output: "result"},
		},
		[]schema.OutputPort{{Name: "result", Type: "string"}},
	)
	deps := &fakeDeps{approve: false, approvedBy: "kevin", serviceResult: "hello"}
	_, err := Interpret(nb, nil, nil, deps)
	if err == nil {
		t.Fatal("expected rejection to stop execution with an error")
	}
}

func TestInterpretMemoryRoundTrip(t *testing.T) {
	nb := simpleBot(
		[]schema.Step{{Name: "remember", Type: "memory.put", Key: "k", Value: "v", Output: "stored"}},
		[]schema.OutputPort{{Name: "stored", Type: "string"}},
	)
	deps := &fakeDeps{}
	res, err := Interpret(nb, nil, nil, deps)
	if err != nil {
		t.Fatalf("Interpret: %v", err)
	}
	if res.Outputs["stored"] != "v" {
		t.Errorf("outputs[stored] = %v, want v", res.Outputs["stored"])
	}
	if got, ok, _ := deps.MemoryGet("test-bot", "k"); !ok || got != "v" {
		t.Errorf("MemoryGet after put = %v, %v, want v, true", got, ok)
	}
}

func TestInterpretServiceCallErrorPropagates(t *testing.T) {
	nb := simpleBot(
		[]schema.Step{{Name: "call", Type: "service.call", Service: "svc", Op: "op", Output: "result"}},
		[]schema.OutputPort{{Name: "result", Type: "string"}},
	)
	deps := &fakeDeps{serviceErr: fmt.Errorf("boom")}
	_, err := Interpret(nb, nil, nil, deps)
	if err == nil {
		t.Fatal("expected the service.call error to propagate")
	}
}

func TestInterpretApproveRejectionWithBoundOutputDoesNotAbort(t *testing.T) {
	nb := simpleBot(
		[]schema.Step{{
			Name: "gate", Type: "approve", Summary: "send it?",
			Outputs: map[string]string{
				"approved":   "{{steps.gate.output.approved}}",
				"decided_by": "{{steps.gate.output.decided_by}}",
			},
		}},
		[]schema.OutputPort{{Name: "approved", Type: "boolean"}, {Name: "decided_by", Type: "string"}},
	)
	deps := &fakeDeps{approve: false, approvedBy: "kevin"}
	res, err := Interpret(nb, nil, nil, deps)
	if err != nil {
		t.Fatalf("Interpret: %v, want no error — a bound-output approve step reports rejection as data", err)
	}
	if res.Outputs["approved"] != false {
		t.Errorf("approved = %#v, want false", res.Outputs["approved"])
	}
	if res.Outputs["decided_by"] != "kevin" {
		t.Errorf("decided_by = %#v, want kevin", res.Outputs["decided_by"])
	}
}

func TestInterpretNotifyProducesBareBool(t *testing.T) {
	nb := simpleBot(
		[]schema.Step{{Name: "send", Type: "notify", Params: map[string]any{"message": "hi", "channel": "slack"}, Output: "delivered"}},
		[]schema.OutputPort{{Name: "delivered", Type: "boolean"}},
	)
	res, err := Interpret(nb, nil, nil, &fakeDeps{})
	if err != nil {
		t.Fatalf("Interpret: %v", err)
	}
	if res.Outputs["delivered"] != true {
		t.Errorf("delivered = %#v, want bare true", res.Outputs["delivered"])
	}
}

func TestInterpretMultiOutputStep(t *testing.T) {
	nb := simpleBot(
		[]schema.Step{{
			Name: "list", Type: "service.call", Service: "svc", Op: "op",
			Outputs: map[string]string{
				"file_id": "{{steps.list.output.id}}",
				"event":   "{{steps.list.output}}",
			},
		}},
		[]schema.OutputPort{{Name: "file_id", Type: "string"}, {Name: "event", Type: "json"}},
	)
	deps := &fakeDeps{serviceResult: map[string]any{"id": "f-1", "name": "recap.pdf"}}
	res, err := Interpret(nb, nil, nil, deps)
	if err != nil {
		t.Fatalf("Interpret: %v", err)
	}
	if res.Outputs["file_id"] != "f-1" {
		t.Errorf("file_id = %v, want f-1", res.Outputs["file_id"])
	}
	if _, ok := res.Outputs["event"].(map[string]any); !ok {
		t.Errorf("event = %#v, want the whole step result", res.Outputs["event"])
	}
}

func TestInterpretTransformPick(t *testing.T) {
	nb := simpleBot(
		[]schema.Step{{
			Name: "shape", Type: "transform.pick",
			Data:   map[string]any{"type": "drive.file.created", "who": "{{inputs.who}}"},
			Output: "event",
		}},
		[]schema.OutputPort{{Name: "event", Type: "json"}},
	)
	inputs := map[string]any{"who": "kevin"}
	res, err := Interpret(nb, inputs, nil, &fakeDeps{})
	if err != nil {
		t.Fatalf("Interpret: %v", err)
	}
	evt, ok := res.Outputs["event"].(map[string]any)
	if !ok || evt["who"] != "kevin" || evt["type"] != "drive.file.created" {
		t.Errorf("event = %#v", res.Outputs["event"])
	}
}

func TestMaybeMaterializeFileFromServiceCall(t *testing.T) {
	nb := simpleBot(
		[]schema.Step{{Name: "download", Type: "service.call", Service: "svc", Op: "files.download", Output: "file"}},
		[]schema.OutputPort{{Name: "file", Type: "file"}},
	)
	blobs, err := NewFSBlobStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	content := base64.StdEncoding.EncodeToString([]byte("hello file"))
	deps := &fakeDeps{
		serviceResult: map[string]any{"content_base64": content, "mime": "text/plain"},
		blobs:         blobs,
	}
	res, err := Interpret(nb, nil, nil, deps)
	if err != nil {
		t.Fatalf("Interpret: %v", err)
	}
	fv, ok := res.Outputs["file"].(FileValue)
	if !ok {
		t.Fatalf("file output = %#v, want a FileValue", res.Outputs["file"])
	}
	got, err := blobs.Read(fv.URI)
	if err != nil || string(got) != "hello file" {
		t.Errorf("blob content = %q, %v", got, err)
	}
}

func TestInterpretUnknownServiceErrors(t *testing.T) {
	nb := simpleBot(
		[]schema.Step{{Name: "call", Type: "service.call", Service: "does-not-exist", Op: "op", Output: "result"}},
		[]schema.OutputPort{{Name: "result", Type: "string"}},
	)
	_, err := Interpret(nb, nil, nil, &fakeDeps{})
	if err == nil {
		t.Fatal("expected an error for an undeclared service reference")
	}
}

func TestInterpretSwarmVarsAreResolvable(t *testing.T) {
	nb := simpleBot(
		[]schema.Step{{
			Name: "call", Type: "service.call", Service: "svc", Op: "op",
			Params: map[string]any{"folder": "{{swarm.vars.recap_folder}}"},
			Output: "result",
		}},
		[]schema.OutputPort{{Name: "result", Type: "string"}},
	)
	wrapped := &paramCapturingDeps{fakeDeps: &fakeDeps{serviceResult: "ok"}}
	swarmVars := map[string]any{"recap_folder": "Recaps/2026"}
	if _, err := Interpret(nb, nil, swarmVars, wrapped); err != nil {
		t.Fatalf("Interpret: %v", err)
	}
	if got := wrapped.lastParams["folder"]; got != "Recaps/2026" {
		t.Errorf("swarm.vars.recap_folder resolved to %#v, want Recaps/2026", got)
	}
}

// paramCapturingDeps wraps fakeDeps just to record the params a service.call
// step actually resolved, since fakeDeps itself ignores them.
type paramCapturingDeps struct {
	*fakeDeps
	lastParams map[string]any
}

func (p *paramCapturingDeps) ServiceCall(svc schema.Service, op string, params map[string]any) (any, error) {
	p.lastParams = params
	return p.fakeDeps.ServiceCall(svc, op, params)
}
