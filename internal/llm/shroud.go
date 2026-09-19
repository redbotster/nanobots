package llm

import (
	"context"

	"github.com/redbotster/nanobots/internal/oneclaw"
	"github.com/redbotster/nanobots/internal/schema"
)

// Shroud routes generation through 1Claw's proxy, which is the only backend
// here that does anything other than forward the prompt: it bills tokens
// against the agent's daily budget, redacts PII and secrets on the way out,
// and screens for prompt injection — all configured per bot from its own
// guardrails (see runner.agentRequestFor).
//
// It also does not substitute models. Every other backend has to, because a
// bot declaring `anthropic/claude-sonnet-4-6` cannot be served by a Gemini
// key. Shroud has no such problem: the agent is created with
// AllowedProviders set to the bot's own declared provider, so the bot's
// model is by construction the one this agent is allowed to call. Asking
// for anything else returns 403 "provider not allowed by agent policy" —
// which is the guardrail working, not an error to route around.
type Shroud struct {
	Client *oneclaw.ShroudClient
}

func NewShroud(c *oneclaw.ShroudClient) *Shroud { return &Shroud{Client: c} }

func (s *Shroud) Describe() string { return "1claw shroud (token billing)" }

func (s *Shroud) Generate(_ context.Context, prompt string, model schema.Model) (string, error) {
	return s.Client.Chat(model.Provider, model.Name, prompt, maxTokensFor(model))
}

// GenerateWithTools is agent.loop's own path through Shroud — real,
// verified against production (docs/1claw-feature-requests.md #13): tools
// + tool_choice: auto returns finish_reason "tool_calls" with a real
// function name/arguments, and a "tool" role reply round-trips back to a
// final answer.
func (s *Shroud) GenerateWithTools(_ context.Context, messages []Message, tools []ToolDef, model schema.Model) (*ToolCallResult, error) {
	resp, err := s.Client.ChatWithTools(model.Provider, model.Name, toShroudMessages(messages), toShroudTools(tools), maxTokensFor(model))
	if err != nil {
		return nil, err
	}
	return fromShroudResponse(resp), nil
}

func toShroudMessages(in []Message) []oneclaw.ChatMessage {
	out := make([]oneclaw.ChatMessage, len(in))
	for i, m := range in {
		out[i] = oneclaw.ChatMessage{
			Role: m.Role, Content: m.Content, ToolCallID: m.ToolCallID,
			ToolCalls: toShroudToolCalls(m.ToolCalls),
		}
	}
	return out
}

func toShroudToolCalls(in []ToolCall) []oneclaw.ChatToolCall {
	if len(in) == 0 {
		return nil
	}
	out := make([]oneclaw.ChatToolCall, len(in))
	for i, c := range in {
		out[i] = oneclaw.ChatToolCall{
			ID: c.ID, Type: "function",
			Function: oneclaw.ChatToolCallFunc{Name: c.Name, Arguments: c.Arguments},
		}
	}
	return out
}

func toShroudTools(in []ToolDef) []oneclaw.ChatTool {
	if len(in) == 0 {
		return nil
	}
	out := make([]oneclaw.ChatTool, len(in))
	for i, t := range in {
		out[i] = oneclaw.ChatTool{Type: "function", Function: oneclaw.ChatToolSpec{
			Name: t.Name, Description: t.Description, Parameters: t.Parameters,
		}}
	}
	return out
}

func fromShroudResponse(resp *oneclaw.ChatToolResponse) *ToolCallResult {
	out := &ToolCallResult{Content: resp.Message.Content, Done: resp.FinishReason != "tool_calls"}
	for _, c := range resp.Message.ToolCalls {
		out.ToolCalls = append(out.ToolCalls, ToolCall{ID: c.ID, Name: c.Function.Name, Arguments: c.Function.Arguments})
	}
	return out
}

var (
	_ Generator  = (*Shroud)(nil)
	_ ToolCaller = (*Shroud)(nil)
)

// DeferredShroud means "use 1Claw Shroud, once this bot's agent exists".
//
// The backend is chosen at startup, but a Shroud client is per-agent and a
// bot's agent is provisioned when the run reaches it. So startup leaves
// this marker and internal/runner swaps in a real Shroud per bot — exactly
// the arrangement memory.DeferredOneClaw uses, and for the same reason.
//
// Until then it behaves as Fallback, so a component that generates outside
// a bot run (the composer, say) still works, and nothing depends on the
// order the two happen in.
type DeferredShroud struct{ Fallback Generator }

func (d *DeferredShroud) Describe() string { return "1claw shroud (token billing)" }

func (d *DeferredShroud) Generate(ctx context.Context, prompt string, model schema.Model) (string, error) {
	if d.Fallback == nil {
		return "", ErrNoGenerator
	}
	return d.Fallback.Generate(ctx, prompt, model)
}

// IsDeferredShroud reports whether g is waiting for an agent.
func IsDeferredShroud(g Generator) bool {
	_, ok := g.(*DeferredShroud)
	return ok
}

var _ Generator = (*DeferredShroud)(nil)
