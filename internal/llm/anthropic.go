package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/redbotster/nanobots/internal/schema"
)

// Anthropic talks to api.anthropic.com's Messages API directly.
//
// No 1Claw in the path, which means no budget ceiling, no PII redaction and
// no injection screening — the three things Shroud adds. That trade is the
// user's to make and it is stated in docs/llm.md and in Settings, not
// hidden here.
type Anthropic struct {
	APIKey string
	// BaseURL defaults to https://api.anthropic.com. Overridable for a
	// gateway that speaks the same shape, and for tests.
	BaseURL string
	// Model is used when a bot asks for a provider this backend doesn't
	// serve. See resolveModel.
	Model string

	HTTPClient *http.Client
	// OnSubstitute, when set, is told each time a bot's declared model was
	// swapped for this backend's own. The runner points it at the run log
	// so a downgrade is never silent.
	OnSubstitute func(note string)
}

const (
	anthropicDefaultBase  = "https://api.anthropic.com"
	anthropicDefaultModel = "claude-sonnet-4-6"
	// anthropicVersion is required on every request; the API rejects a
	// call without it rather than assuming a default.
	anthropicVersion = "2023-06-01"
)

func NewAnthropic(apiKey, model string) *Anthropic {
	if model == "" {
		model = anthropicDefaultModel
	}
	return &Anthropic{APIKey: apiKey, BaseURL: anthropicDefaultBase, Model: model, HTTPClient: newHTTPClient()}
}

func (a *Anthropic) Describe() string { return "anthropic (direct)" }

type anthropicRequest struct {
	Model       string             `json:"model"`
	MaxTokens   int                `json:"max_tokens"`
	Temperature *float64           `json:"temperature,omitempty"`
	Messages    []anthropicMessage `json:"messages"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

func (a *Anthropic) Generate(ctx context.Context, prompt string, model schema.Model) (string, error) {
	name, substituted := resolveModel("anthropic", a.Model, model)
	if substituted && a.OnSubstitute != nil {
		if note := substitutionNote(model, "anthropic", name); note != "" {
			a.OnSubstitute(note)
		}
	}
	req := anthropicRequest{
		Model:     name,
		MaxTokens: maxTokensFor(model),
		Messages:  []anthropicMessage{{Role: "user", Content: prompt}},
	}
	// Only sent when the bot set one: zero is a legitimate temperature
	// meaning "be deterministic", and sending it for every bot that simply
	// omitted the field would change how the whole catalog behaves.
	if model.Temperature != 0 {
		t := model.Temperature
		req.Temperature = &t
	}

	var out anthropicResponse
	err := postJSON(ctx, a.client(), strings.TrimRight(a.BaseURL, "/")+"/v1/messages", map[string]string{
		"x-api-key":         a.APIKey,
		"anthropic-version": anthropicVersion,
	}, req, &out)
	if err != nil {
		return "", err
	}
	// A response can carry several blocks (text, thinking, tool_use); join
	// the text ones rather than assuming the first block is the answer.
	var b strings.Builder
	for _, c := range out.Content {
		if c.Type == "text" {
			b.WriteString(c.Text)
		}
	}
	if b.Len() == 0 {
		return "", fmt.Errorf("llm: anthropic returned no text content")
	}
	return b.String(), nil
}

func (a *Anthropic) client() *http.Client {
	if a.HTTPClient != nil {
		return a.HTTPClient
	}
	return newHTTPClient()
}

// anthropicContentBlock is Anthropic's own richer message shape: unlike
// OpenAI's flat string, a Messages API turn is an array of typed blocks —
// text, tool_use (the model calling something) or tool_result (a tool's
// answer going back). Fields are optional per type and left as pointers
// where a zero value would be ambiguous (Input's absence vs. an empty
// object) rather than because Anthropic's own schema requires it.
type anthropicContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
	// tool_use fields
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
	// tool_result fields — Anthropic has no "tool" role; a result is a
	// "user" turn carrying this block instead.
	ToolUseID string `json:"tool_use_id,omitempty"`
	Content   string `json:"content,omitempty"`
}

type anthropicToolMessage struct {
	Role    string                  `json:"role"`
	Content []anthropicContentBlock `json:"content"`
}

type anthropicTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"input_schema,omitempty"`
}

type anthropicToolRequest struct {
	Model     string                 `json:"model"`
	MaxTokens int                    `json:"max_tokens"`
	Messages  []anthropicToolMessage `json:"messages"`
	Tools     []anthropicTool        `json:"tools,omitempty"`
}

type anthropicToolResponse struct {
	Content    []anthropicContentBlock `json:"content"`
	StopReason string                  `json:"stop_reason"`
}

// GenerateWithTools is agent.loop's path through a direct Anthropic key —
// the Messages API's own tool-calling shape, distinct enough from OpenAI's
// flat "tool" role (Shroud's shape, since it forwards OpenAI-shaped bodies
// unmodified) that this backend does its own translation rather than
// sharing Shroud's.
func (a *Anthropic) GenerateWithTools(ctx context.Context, messages []Message, tools []ToolDef, model schema.Model) (*ToolCallResult, error) {
	name, substituted := resolveModel("anthropic", a.Model, model)
	if substituted && a.OnSubstitute != nil {
		if note := substitutionNote(model, "anthropic", name); note != "" {
			a.OnSubstitute(note)
		}
	}
	req := anthropicToolRequest{
		Model: name, MaxTokens: maxTokensFor(model),
		Messages: toAnthropicMessages(messages),
		Tools:    toAnthropicTools(tools),
	}
	var out anthropicToolResponse
	err := postJSON(ctx, a.client(), strings.TrimRight(a.BaseURL, "/")+"/v1/messages", map[string]string{
		"x-api-key":         a.APIKey,
		"anthropic-version": anthropicVersion,
	}, req, &out)
	if err != nil {
		return nil, err
	}
	return fromAnthropicResponse(out), nil
}

func toAnthropicMessages(in []Message) []anthropicToolMessage {
	out := make([]anthropicToolMessage, 0, len(in))
	for _, m := range in {
		switch m.Role {
		case "tool":
			// No "tool" role in Anthropic's model — a result is a "user"
			// turn carrying a tool_result block instead.
			out = append(out, anthropicToolMessage{Role: "user", Content: []anthropicContentBlock{
				{Type: "tool_result", ToolUseID: m.ToolCallID, Content: m.Content},
			}})
		case "assistant":
			var blocks []anthropicContentBlock
			if m.Content != "" {
				blocks = append(blocks, anthropicContentBlock{Type: "text", Text: m.Content})
			}
			for _, c := range m.ToolCalls {
				// Anthropic wants input as a real JSON object, not the raw
				// string OpenAI's arguments field is — invalid JSON here
				// (which shouldn't happen, since it's an echo of what
				// Anthropic itself sent) becomes an empty object rather
				// than silently dropping the call.
				var input json.RawMessage
				if json.Valid([]byte(c.Arguments)) {
					input = json.RawMessage(c.Arguments)
				} else {
					input = json.RawMessage("{}")
				}
				blocks = append(blocks, anthropicContentBlock{Type: "tool_use", ID: c.ID, Name: c.Name, Input: input})
			}
			out = append(out, anthropicToolMessage{Role: "assistant", Content: blocks})
		default:
			out = append(out, anthropicToolMessage{Role: m.Role, Content: []anthropicContentBlock{
				{Type: "text", Text: m.Content},
			}})
		}
	}
	return out
}

func toAnthropicTools(in []ToolDef) []anthropicTool {
	if len(in) == 0 {
		return nil
	}
	out := make([]anthropicTool, len(in))
	for i, t := range in {
		out[i] = anthropicTool{Name: t.Name, Description: t.Description, InputSchema: t.Parameters}
	}
	return out
}

func fromAnthropicResponse(resp anthropicToolResponse) *ToolCallResult {
	out := &ToolCallResult{Done: resp.StopReason != "tool_use"}
	var text strings.Builder
	for _, c := range resp.Content {
		switch c.Type {
		case "text":
			text.WriteString(c.Text)
		case "tool_use":
			out.ToolCalls = append(out.ToolCalls, ToolCall{ID: c.ID, Name: c.Name, Arguments: string(c.Input)})
		}
	}
	out.Content = text.String()
	return out
}

var (
	_ Generator  = (*Anthropic)(nil)
	_ ToolCaller = (*Anthropic)(nil)
)
