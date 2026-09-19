package llm

import (
	"context"
	"errors"

	"github.com/redbotster/nanobots/internal/schema"
)

// Message is one turn in a tool-calling conversation — the same three
// shapes OpenAI's chat/completions format uses, which is what Shroud
// forwards unmodified (docs/1claw-feature-requests.md #13).
// Message, ToolDef, ToolCall and ToolCallResult all carry JSON tags for one
// reason beyond talking to a backend: RemoteDeps.GenerateWithTools ships
// them verbatim to nanobotd's /internal/steps/agent_generate callback and
// back, the same round trip every other step type's params already make.
type Message struct {
	Role string `json:"role"` // "user" | "assistant" | "tool"
	// Content is empty on an assistant message that only carries tool
	// calls.
	Content string `json:"content,omitempty"`
	// ToolCalls is set on an assistant message that called one or more
	// tools — round-tripped back on the next request so the model sees
	// its own prior call.
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	// ToolCallID is set on a "tool" role message: which call this is the
	// result of.
	ToolCallID string `json:"tool_call_id,omitempty"`
}

// ToolDef describes one tool the model may call: a name, a description,
// and its arguments as a JSON Schema object — OpenAI/Anthropic's shared
// function-calling shape.
type ToolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"` // JSON Schema
}

// ToolCall is one function call the model asked for.
type ToolCall struct {
	ID string `json:"id"`
	// Name and Arguments are copied whole from the model's own answer.
	// Arguments is raw JSON text — what a valid shape is depends entirely
	// on the tool, which this package knows nothing about.
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ToolCallResult is what one tool-calling turn produced: final text, or
// one or more tool calls the caller must satisfy (append the assistant
// message plus a "tool" role reply per call, and ask again) before it has
// an answer.
type ToolCallResult struct {
	Content   string     `json:"content,omitempty"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	// Done is true when the model produced a final answer rather than a
	// tool call. A backend could in principle set both Content and
	// ToolCalls (some models narrate before calling a tool); Done is what
	// callers should actually branch on.
	Done bool `json:"done"`
}

// ToolCaller is optional on a Generator — a backend advertises it by
// implementing it, the identical pattern memory.Recaller uses on
// memory.Store. Not every backend can do this: it needs a request shape
// richer than one prompt in, one string out, which Generator alone can't
// express.
type ToolCaller interface {
	GenerateWithTools(ctx context.Context, messages []Message, tools []ToolDef, model schema.Model) (*ToolCallResult, error)
}

// ErrNoToolCalling is what an agent.loop step meets when the configured
// backend can't do tool use at all — named so callers can tell "this
// deployment's LLM can't do that" from "the model declined to call
// anything", which is a completely different, unremarkable outcome.
var ErrNoToolCalling = errors.New("this LLM backend does not support tool-calling — agent.loop needs " +
	"Shroud or a direct Anthropic key (see docs/agent-loop.md)")

// ToolCallerOf returns g as a ToolCaller, or false if this backend only
// does plain single-shot generation.
func ToolCallerOf(g Generator) (ToolCaller, bool) {
	tc, ok := g.(ToolCaller)
	return tc, ok
}

// GenerateWithTools is the one call sites should use: it asks g for a
// tool-calling turn, giving a clear error when the backend can't.
func GenerateWithTools(ctx context.Context, g Generator, messages []Message, tools []ToolDef, model schema.Model) (*ToolCallResult, error) {
	tc, ok := ToolCallerOf(g)
	if !ok {
		return nil, ErrNoToolCalling
	}
	return tc.GenerateWithTools(ctx, messages, tools, model)
}
