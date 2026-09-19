package step

import (
	"strings"
	"testing"

	"github.com/redbotster/nanobots/internal/llm"
	"github.com/redbotster/nanobots/internal/schema"
)

func agentLoopBot(steps []schema.Step, services []schema.Service, outputs []schema.OutputPort) *schema.Nanobot {
	return &schema.Nanobot{
		Metadata: schema.Metadata{Name: "test-bot"},
		Spec: schema.NanobotSpec{
			Services: services,
			Ports:    schema.Ports{Outputs: outputs},
			Steps:    steps,
		},
	}
}

// The simplest real case: the model needs no tool at all and answers on
// the first turn.
func TestAgentLoopReturnsAFinalAnswerWithNoToolCalls(t *testing.T) {
	nb := agentLoopBot(
		[]schema.Step{{
			Name: "research", Type: "agent.loop", Goal: "what is 2+2?",
			MaxIterations: 5, Output: "answer",
		}}, nil, []schema.OutputPort{{Name: "answer", Type: "string"}},
	)
	deps := &fakeDeps{agentLoopTurns: []*llm.ToolCallResult{
		{Content: "4", Done: true},
	}}
	res, err := Interpret(nb, nil, nil, deps)
	if err != nil {
		t.Fatalf("Interpret: %v", err)
	}
	if res.Outputs["answer"] != "4" {
		t.Errorf("outputs[answer] = %v, want 4", res.Outputs["answer"])
	}
	if len(deps.gotToolMessages) != 1 {
		t.Fatalf("called GenerateWithTools %d times, want 1", len(deps.gotToolMessages))
	}
	if deps.gotToolMessages[0][0].Role != "user" || deps.gotToolMessages[0][0].Content != "what is 2+2?" {
		t.Errorf("first message = %+v, want the goal as a user turn", deps.gotToolMessages[0][0])
	}
}

// A builtin tool call, its result round-tripped back as a "tool" message,
// then a final answer on the next turn.
func TestAgentLoopDispatchesABuiltinToolAndContinues(t *testing.T) {
	nb := agentLoopBot(
		[]schema.Step{{
			Name: "research", Type: "agent.loop", Goal: "what's on example.com?",
			Tools:         []schema.AgentTool{{Name: "fetch", Builtin: "web.fetch"}},
			MaxIterations: 5, Output: "answer",
		}}, nil, []schema.OutputPort{{Name: "answer", Type: "string"}},
	)
	deps := &fakeDeps{
		serviceResult: map[string]any{"body": "hello world"},
		agentLoopTurns: []*llm.ToolCallResult{
			{ToolCalls: []llm.ToolCall{{ID: "c1", Name: "fetch", Arguments: `{"url":"https://example.com"}`}}},
			{Content: "It says hello world.", Done: true},
		},
	}
	res, err := Interpret(nb, nil, nil, deps)
	if err != nil {
		t.Fatalf("Interpret: %v", err)
	}
	if res.Outputs["answer"] != "It says hello world." {
		t.Errorf("outputs[answer] = %v", res.Outputs["answer"])
	}
	// The second call's messages must include the assistant's tool call
	// and the tool's own result — the wire the model actually needs.
	second := deps.gotToolMessages[1]
	if len(second) != 3 {
		t.Fatalf("second call had %d messages, want 3 (user, assistant tool_calls, tool result): %+v", len(second), second)
	}
	if second[1].Role != "assistant" || len(second[1].ToolCalls) != 1 {
		t.Errorf("message[1] = %+v, want the assistant's tool call echoed back", second[1])
	}
	if second[2].Role != "tool" || second[2].ToolCallID != "c1" || !strings.Contains(second[2].Content, "hello world") {
		t.Errorf("message[2] = %+v, want the tool's own result", second[2])
	}
}

// A tool backed by a declared service dispatches through the exact same
// deps.ServiceCall path a fixed service.call step uses — connection: demo
// answers from fixtures either way, which this proves by using fakeDeps's
// serviceResult the same way TestInterpretServiceCallBindsOutput does.
func TestAgentLoopDispatchesAServiceTool(t *testing.T) {
	nb := agentLoopBot(
		[]schema.Step{{
			Name: "research", Type: "agent.loop", Goal: "list files",
			Tools:         []schema.AgentTool{{Name: "list_files", Service: "gdrive", Op: "files.list"}},
			MaxIterations: 5, Output: "answer",
		}},
		[]schema.Service{{ID: "gdrive", Provider: "google"}},
		[]schema.OutputPort{{Name: "answer", Type: "string"}},
	)
	deps := &fakeDeps{
		serviceResult: []any{"a.txt", "b.txt"},
		agentLoopTurns: []*llm.ToolCallResult{
			{ToolCalls: []llm.ToolCall{{ID: "c1", Name: "list_files", Arguments: `{}`}}},
			{Content: "a.txt, b.txt", Done: true},
		},
	}
	res, err := Interpret(nb, nil, nil, deps)
	if err != nil {
		t.Fatalf("Interpret: %v", err)
	}
	if res.Outputs["answer"] != "a.txt, b.txt" {
		t.Errorf("outputs[answer] = %v", res.Outputs["answer"])
	}
}

// The exact hazard docs/error-policy.md's CheckRetry already refuses for
// an ordinary retry: on a writing bot — a write must be held behind the
// same deps.Approve gate bots/email-send-approved already uses inline,
// not silently performed because a model happened to choose it.
func TestAgentLoopGatesAWritingToolBehindApproval(t *testing.T) {
	nb := agentLoopBot(
		[]schema.Step{{
			Name: "assistant", Type: "agent.loop", Goal: "email the summary",
			Tools: []schema.AgentTool{{
				Name: "send_email", Service: "gmail", Op: "messages.send", Writes: "send an email",
			}},
			MaxIterations: 5, Output: "answer",
		}},
		[]schema.Service{{ID: "gmail", Provider: "google"}},
		[]schema.OutputPort{{Name: "answer", Type: "string"}},
	)

	t.Run("approved, the call proceeds", func(t *testing.T) {
		deps := &fakeDeps{
			approve: true, approvedBy: "kevin", serviceResult: map[string]any{"sent": true},
			agentLoopTurns: []*llm.ToolCallResult{
				{ToolCalls: []llm.ToolCall{{ID: "c1", Name: "send_email", Arguments: `{"to":"a@b.com"}`}}},
				{Content: "sent", Done: true},
			},
		}
		res, err := Interpret(nb, nil, nil, deps)
		if err != nil {
			t.Fatalf("Interpret: %v", err)
		}
		if res.Outputs["answer"] != "sent" {
			t.Errorf("outputs[answer] = %v", res.Outputs["answer"])
		}
		if deps.approveSummary == "" || !strings.Contains(deps.approveSummary, "send_email") {
			t.Errorf("approval summary = %q, want it to name the tool", deps.approveSummary)
		}
		if deps.approveRiskTier != "high" {
			t.Errorf("risk tier = %q, want high", deps.approveRiskTier)
		}
	})

	t.Run("declined, the step fails rather than performing the write", func(t *testing.T) {
		deps := &fakeDeps{
			approve: false, approvedBy: "kevin",
			agentLoopTurns: []*llm.ToolCallResult{
				{ToolCalls: []llm.ToolCall{{ID: "c1", Name: "send_email", Arguments: `{"to":"a@b.com"}`}}},
			},
		}
		_, err := Interpret(nb, nil, nil, deps)
		if err == nil {
			t.Fatal("expected a declined approval to fail the step")
		}
		if !strings.Contains(err.Error(), "not approved") {
			t.Errorf("err = %v, want it to say not approved", err)
		}
	})
}

// A tool name the model invented (or that was declared with a typo it
// never noticed) is told back to the model as a normal tool failure, not a
// panic or an aborted run — the model gets to try something else.
func TestAgentLoopRefusesAnUndeclaredToolWithoutFailingTheRun(t *testing.T) {
	nb := agentLoopBot(
		[]schema.Step{{
			Name: "research", Type: "agent.loop", Goal: "look something up",
			Tools:         []schema.AgentTool{{Name: "fetch", Builtin: "web.fetch"}},
			MaxIterations: 5, Output: "answer",
		}}, nil, []schema.OutputPort{{Name: "answer", Type: "string"}},
	)
	deps := &fakeDeps{
		serviceResult: map[string]any{"body": "ok"},
		agentLoopTurns: []*llm.ToolCallResult{
			{ToolCalls: []llm.ToolCall{{ID: "c1", Name: "delete_everything", Arguments: `{}`}}},
			{Content: "gave up on that, here's what I know anyway", Done: true},
		},
	}
	res, err := Interpret(nb, nil, nil, deps)
	if err != nil {
		t.Fatalf("Interpret: %v, want the run to survive an undeclared tool call", err)
	}
	if res.Outputs["answer"] == "" {
		t.Error("expected a final answer despite the refused tool call")
	}
	second := deps.gotToolMessages[1]
	toolMsg := second[len(second)-1]
	if toolMsg.Role != "tool" || !strings.Contains(toolMsg.Content, "error") {
		t.Errorf("tool result for the undeclared call = %+v, want an error the model can see", toolMsg)
	}
}

// The cap exists so an LLM's own judgement about "one more tool call" is
// never the only bound — and hitting it must fail loudly with what was
// tried, never silently return the last partial thing.
func TestAgentLoopFailsLoudlyWhenItHitsMaxIterations(t *testing.T) {
	nb := agentLoopBot(
		[]schema.Step{{
			Name: "research", Type: "agent.loop", Goal: "keep going forever",
			Tools:         []schema.AgentTool{{Name: "fetch", Builtin: "web.fetch"}},
			MaxIterations: 2, Output: "answer",
		}}, nil, []schema.OutputPort{{Name: "answer", Type: "string"}},
	)
	deps := &fakeDeps{
		serviceResult: map[string]any{"body": "more"},
		agentLoopTurns: []*llm.ToolCallResult{
			{ToolCalls: []llm.ToolCall{{ID: "c1", Name: "fetch", Arguments: `{}`}}},
			{ToolCalls: []llm.ToolCall{{ID: "c2", Name: "fetch", Arguments: `{}`}}},
		},
	}
	_, err := Interpret(nb, nil, nil, deps)
	if err == nil {
		t.Fatal("expected hitting max_iterations to fail the step")
	}
	if !strings.Contains(err.Error(), "cap of 2") {
		t.Errorf("err = %v, want it to name the cap", err)
	}
	if !strings.Contains(err.Error(), "fetch") {
		t.Errorf("err = %v, want the partial transcript attached", err)
	}
}

func TestAgentLoopNeedsAGoal(t *testing.T) {
	nb := agentLoopBot(
		[]schema.Step{{Name: "research", Type: "agent.loop", MaxIterations: 5, Output: "answer"}},
		nil, []schema.OutputPort{{Name: "answer", Type: "string"}},
	)
	deps := &fakeDeps{}
	if _, err := Interpret(nb, nil, nil, deps); err == nil {
		t.Fatal("expected a missing goal to be an error")
	}
}

func TestAgentLoopNeedsAPositiveMaxIterations(t *testing.T) {
	nb := agentLoopBot(
		[]schema.Step{{Name: "research", Type: "agent.loop", Goal: "go", Output: "answer"}},
		nil, []schema.OutputPort{{Name: "answer", Type: "string"}},
	)
	deps := &fakeDeps{agentLoopTurns: []*llm.ToolCallResult{{Content: "x", Done: true}}}
	if _, err := Interpret(nb, nil, nil, deps); err == nil {
		t.Fatal("expected max_iterations: 0 to be an error even without the planner catching it first")
	}
}
