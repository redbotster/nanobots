package oneclaw

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestShroudChatSendsExpectedHeadersAndParsesResponse(t *testing.T) {
	var gotAgentKey, gotProvider string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAgentKey = r.Header.Get("X-Shroud-Agent-Key")
		gotProvider = r.Header.Get("X-Shroud-Provider")
		json.NewDecoder(r.Body).Decode(&gotBody)
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"role": "assistant", "content": "hello back"}},
			},
		})
	}))
	defer srv.Close()

	c := NewShroudClient("agent-1", "ocv_key")
	c.BaseURL = srv.URL
	reply, err := c.Chat("anthropic", "claude-sonnet-4-6", "hi", 100)
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if reply != "hello back" {
		t.Errorf("reply = %q, want %q", reply, "hello back")
	}
	if gotAgentKey != "agent-1:ocv_key" {
		t.Errorf("X-Shroud-Agent-Key = %q, want agent-1:ocv_key", gotAgentKey)
	}
	if gotProvider != "anthropic" {
		t.Errorf("X-Shroud-Provider = %q, want anthropic", gotProvider)
	}
	if gotBody["model"] != "claude-sonnet-4-6" {
		t.Errorf("body model = %v, want claude-sonnet-4-6", gotBody["model"])
	}
}

// v3 Phase 3: the shape verified live against production
// (docs/1claw-feature-requests.md #13) — tools + tool_choice: auto returns
// finish_reason "tool_calls" with a real function name/arguments.
func TestChatWithToolsSendsToolsAndParsesAToolCall(t *testing.T) {
	var gotProvider string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotProvider = r.Header.Get("X-Shroud-Provider")
		json.NewDecoder(r.Body).Decode(&gotBody)
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{
					"message": map[string]any{
						"role": "assistant",
						"tool_calls": []map[string]any{
							{
								"id":   "call_1",
								"type": "function",
								"function": map[string]any{
									"name":      "get_weather",
									"arguments": `{"city":"Paris"}`,
								},
							},
						},
					},
					"finish_reason": "tool_calls",
				},
			},
		})
	}))
	defer srv.Close()

	c := NewShroudClient("agent-1", "ocv_key")
	c.BaseURL = srv.URL
	resp, err := c.ChatWithTools("anthropic", "claude-sonnet-4-6",
		[]ChatMessage{{Role: "user", Content: "What's the weather in Paris?"}},
		[]ChatTool{{Type: "function", Function: ChatToolSpec{
			Name: "get_weather", Description: "Get current weather",
			Parameters: map[string]any{"type": "object", "properties": map[string]any{
				"city": map[string]any{"type": "string"},
			}},
		}}}, 100)
	if err != nil {
		t.Fatalf("ChatWithTools: %v", err)
	}
	if gotProvider != "anthropic" {
		t.Errorf("X-Shroud-Provider = %q, want anthropic", gotProvider)
	}
	if gotBody["tool_choice"] != "auto" {
		t.Errorf("tool_choice = %v, want auto", gotBody["tool_choice"])
	}
	tools, _ := gotBody["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("sent %d tools, want 1: %v", len(tools), gotBody["tools"])
	}
	if resp.FinishReason != "tool_calls" {
		t.Errorf("FinishReason = %q, want tool_calls", resp.FinishReason)
	}
	if len(resp.Message.ToolCalls) != 1 {
		t.Fatalf("got %d tool calls, want 1", len(resp.Message.ToolCalls))
	}
	call := resp.Message.ToolCalls[0]
	if call.ID != "call_1" || call.Function.Name != "get_weather" || call.Function.Arguments != `{"city":"Paris"}` {
		t.Errorf("tool call = %+v", call)
	}
}

// The result of a tool call round-trips back as a "tool" role message with
// tool_call_id — the shape verified live to make the model answer from it.
func TestChatWithToolsSendsAToolResultMessage(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&gotBody)
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"role": "assistant", "content": "It's 18C in Paris."}, "finish_reason": "stop"},
			},
		})
	}))
	defer srv.Close()

	c := NewShroudClient("agent-1", "ocv_key")
	c.BaseURL = srv.URL
	resp, err := c.ChatWithTools("anthropic", "claude-sonnet-4-6", []ChatMessage{
		{Role: "user", Content: "What's the weather in Paris?"},
		{Role: "assistant", ToolCalls: []ChatToolCall{
			{ID: "call_1", Type: "function", Function: ChatToolCallFunc{Name: "get_weather", Arguments: `{"city":"Paris"}`}},
		}},
		{Role: "tool", ToolCallID: "call_1", Content: `{"tempC":18}`},
	}, nil, 100)
	if err != nil {
		t.Fatalf("ChatWithTools: %v", err)
	}
	if resp.FinishReason != "stop" || resp.Message.Content != "It's 18C in Paris." {
		t.Errorf("resp = %+v", resp)
	}
	msgs, _ := gotBody["messages"].([]any)
	if len(msgs) != 3 {
		t.Fatalf("sent %d messages, want 3: %v", len(msgs), msgs)
	}
	toolMsg, _ := msgs[2].(map[string]any)
	if toolMsg["role"] != "tool" || toolMsg["tool_call_id"] != "call_1" {
		t.Errorf("tool result message = %+v", toolMsg)
	}
}

func TestShroudChatNoChoicesErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{}})
	}))
	defer srv.Close()

	c := NewShroudClient("agent-1", "ocv_key")
	c.BaseURL = srv.URL
	if _, err := c.Chat("anthropic", "claude-sonnet-4-6", "hi", 100); err == nil {
		t.Fatal("expected an error for an empty choices response")
	}
}
