package oneclaw

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// DefaultShroudURL is a var, not a const, purely so tests can point a
// ShroudClient at an httptest server instead of the real Shroud endpoint.
var DefaultShroudURL = "https://shroud.1claw.co"

// ShroudClient talks to 1Claw's Shroud LLM proxy directly — a different host
// than the Human API, authenticated with an agent's own id:api_key pair
// rather than the human bearer token (see docs/oneclaw-bridge.md).
type ShroudClient struct {
	BaseURL    string
	HTTPClient *http.Client
	AgentID    string
	AgentKey   string // ocv_...
}

func NewShroudClient(agentID, agentKey string) *ShroudClient {
	return &ShroudClient{
		BaseURL:    DefaultShroudURL,
		HTTPClient: &http.Client{Timeout: 60 * time.Second},
		AgentID:    agentID,
		AgentKey:   agentKey,
	}
}

type shroudMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type shroudChatRequest struct {
	Model     string          `json:"model"`
	Messages  []shroudMessage `json:"messages"`
	MaxTokens int             `json:"max_tokens,omitempty"`
}

type shroudChatResponse struct {
	Choices []struct {
		Message shroudMessage `json:"message"`
	} `json:"choices"`
}

// ChatMessage is one turn in a tool-calling conversation, OpenAI-shaped —
// the shape Shroud's own chat/completions endpoint forwards unmodified
// (docs/1claw-feature-requests.md #13). Content is a plain string on every
// role Shroud has actually been verified against; Anthropic's richer
// content-block array is a different backend's own concern, not this one's.
type ChatMessage struct {
	Role string `json:"role"` // "user" | "assistant" | "tool" | "system"
	// Content is omitted, not sent empty, on an assistant message that only
	// carries tool calls — some backends behind Shroud reject an empty
	// string where they expect null or an absent field entirely.
	Content string `json:"content,omitempty"`
	// ToolCalls is set on an assistant message that called one or more
	// tools — round-tripped back on the next request so the model sees its
	// own prior call.
	ToolCalls []ChatToolCall `json:"tool_calls,omitempty"`
	// ToolCallID is set on a "tool" role message: which call this is the
	// result of.
	ToolCallID string `json:"tool_call_id,omitempty"`
}

// ChatToolCall is one function call the model asked for, or asked for and
// is being told the result of.
type ChatToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"` // always "function" — the only kind Shroud forwards
	Function ChatToolCallFunc `json:"function"`
}

type ChatToolCallFunc struct {
	Name string `json:"name"`
	// Arguments is the model's own JSON-encoded argument object, as a raw
	// string — not decoded here, because what a valid argument shape is
	// depends entirely on the tool, which this package knows nothing about.
	Arguments string `json:"arguments"`
}

// ChatTool describes one tool the model may call, OpenAI's function-calling
// shape: {"type":"function","function":{name, description, parameters}}.
type ChatTool struct {
	Type     string       `json:"type"`
	Function ChatToolSpec `json:"function"`
}

type ChatToolSpec struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"` // JSON Schema
}

type chatCompletionsRequest struct {
	Model      string        `json:"model"`
	Messages   []ChatMessage `json:"messages"`
	Tools      []ChatTool    `json:"tools,omitempty"`
	ToolChoice string        `json:"tool_choice,omitempty"`
	MaxTokens  int           `json:"max_tokens,omitempty"`
}

// ChatToolResponse is what one tool-calling turn produced: text, or one or
// more tool calls the caller must satisfy (by appending the assistant's own
// message plus a "tool" role reply per call) and send back for the next
// turn.
type ChatToolResponse struct {
	Message      ChatMessage
	FinishReason string
}

type chatCompletionsResponse struct {
	Choices []struct {
		Message      ChatMessage `json:"message"`
		FinishReason string      `json:"finish_reason"`
	} `json:"choices"`
}

// Chat sends one user-role prompt through Shroud and returns the assistant's
// text response. provider/model come from the bot's declared spec.model.
func (s *ShroudClient) Chat(provider, model, prompt string, maxTokens int) (string, error) {
	body, err := json.Marshal(shroudChatRequest{
		Model:     model,
		Messages:  []shroudMessage{{Role: "user", Content: prompt}},
		MaxTokens: maxTokens,
	})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequest(http.MethodPost, s.BaseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Shroud-Agent-Key", s.AgentID+":"+s.AgentKey)
	req.Header.Set("X-Shroud-Provider", provider)

	resp, err := s.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("shroud: request failed: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("shroud: chat failed (%d): %s", resp.StatusCode, truncate(raw))
	}
	var cr shroudChatResponse
	if err := json.Unmarshal(raw, &cr); err != nil {
		return "", fmt.Errorf("shroud: parse response: %w", err)
	}
	if len(cr.Choices) == 0 {
		return "", fmt.Errorf("shroud: response had no choices (body: %s)", truncate(raw))
	}
	return cr.Choices[0].Message.Content, nil
}

// ChatWithTools sends a multi-turn, tool-calling conversation through
// Shroud and returns the assistant's next turn — either final text or one
// or more tool calls the caller must satisfy and send back.
//
// Verified against production with a real funded key
// (docs/1claw-feature-requests.md #13): tools + tool_choice: "auto" returns
// finish_reason "tool_calls" with real ChatToolCall values; a "tool" role
// message with ToolCallID round-trips and the model answers from it.
// X-Shroud-Provider is required — a call without it 400s, the same header
// Chat already sends.
func (s *ShroudClient) ChatWithTools(provider, model string, messages []ChatMessage, tools []ChatTool, maxTokens int) (*ChatToolResponse, error) {
	body, err := json.Marshal(chatCompletionsRequest{
		Model:      model,
		Messages:   messages,
		Tools:      tools,
		ToolChoice: "auto",
		MaxTokens:  maxTokens,
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, s.BaseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Shroud-Agent-Key", s.AgentID+":"+s.AgentKey)
	req.Header.Set("X-Shroud-Provider", provider)

	resp, err := s.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("shroud: request failed: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("shroud: chat failed (%d): %s", resp.StatusCode, truncate(raw))
	}
	var cr chatCompletionsResponse
	if err := json.Unmarshal(raw, &cr); err != nil {
		return nil, fmt.Errorf("shroud: parse response: %w", err)
	}
	if len(cr.Choices) == 0 {
		return nil, fmt.Errorf("shroud: response had no choices (body: %s)", truncate(raw))
	}
	return &ChatToolResponse{Message: cr.Choices[0].Message, FinishReason: cr.Choices[0].FinishReason}, nil
}
