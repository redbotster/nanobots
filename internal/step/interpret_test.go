package step

import (
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
func (f *fakeDeps) Blobs() BlobStore                     { return &FSBlobStore{} }

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
	res, err := Interpret(nb, nil, deps)
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
	_, err := Interpret(nb, nil, deps)
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
	_, err := Interpret(nb, nil, deps)
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
	_, err := Interpret(nb, nil, deps)
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
	res, err := Interpret(nb, nil, deps)
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
	_, err := Interpret(nb, nil, deps)
	if err == nil {
		t.Fatal("expected the service.call error to propagate")
	}
}

func TestInterpretUnknownServiceErrors(t *testing.T) {
	nb := simpleBot(
		[]schema.Step{{Name: "call", Type: "service.call", Service: "does-not-exist", Op: "op", Output: "result"}},
		[]schema.OutputPort{{Name: "result", Type: "string"}},
	)
	_, err := Interpret(nb, nil, &fakeDeps{})
	if err == nil {
		t.Fatal("expected an error for an undeclared service reference")
	}
}
