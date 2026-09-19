package team

import "testing"

// Every line here is verbatim from a real `gemini -o stream-json` run
// against this repo (docs/team.md's verification), not constructed from
// the tool's own documentation — the same discipline
// internal/foundry/agent_claude_test.go holds ParseStreamJSONLine to.

func TestParseGeminiStreamJSONLineDropsInit(t *testing.T) {
	line := `{"type":"init","timestamp":"2026-09-19T13:38:26.648Z","session_id":"604accbb-a4e8-4e0d-af93-70bd08478aba","model":"auto"}`
	if events := ParseGeminiStreamJSONLine([]byte(line)); events != nil {
		t.Errorf("expected no events for init, got %+v", events)
	}
}

func TestParseGeminiStreamJSONLineDropsTheEchoedUserPrompt(t *testing.T) {
	line := `{"type":"message","timestamp":"2026-09-19T13:38:26.649Z","role":"user","content":"Say hello in exactly three words."}`
	if events := ParseGeminiStreamJSONLine([]byte(line)); events != nil {
		t.Errorf("expected no events for the echoed prompt, got %+v", events)
	}
}

func TestParseGeminiStreamJSONLineSurfacesAssistantText(t *testing.T) {
	line := `{"type":"message","timestamp":"2026-09-19T13:39:09.250Z","role":"assistant","content":"Hello there, user.","delta":true}`
	events := ParseGeminiStreamJSONLine([]byte(line))
	if len(events) != 1 || events[0].Phase != "text" || events[0].Msg != "Hello there, user." {
		t.Fatalf("events = %+v", events)
	}
}

func TestParseGeminiStreamJSONLineSurfacesToolUseWithFilePath(t *testing.T) {
	line := `{"type":"tool_use","timestamp":"2026-09-19T13:40:40.331Z","tool_name":"read_file","tool_id":"read_file__call_586968","parameters":{"end_line":100,"file_path":"docs/anatomy.md","start_line":1}}`
	events := ParseGeminiStreamJSONLine([]byte(line))
	if len(events) != 1 || events[0].Phase != "tool" || events[0].Msg != "read_file docs/anatomy.md" {
		t.Fatalf("events = %+v", events)
	}
}

func TestParseGeminiStreamJSONLineSurfacesToolResult(t *testing.T) {
	line := `{"type":"tool_result","timestamp":"2026-09-19T13:40:40.357Z","tool_id":"read_file__call_586968","status":"success","output":"Read lines 1-100 of 156 from docs/anatomy.md"}`
	events := ParseGeminiStreamJSONLine([]byte(line))
	if len(events) != 1 || events[0].Phase != "tool" || events[0].Msg != "Read lines 1-100 of 156 from docs/anatomy.md" {
		t.Fatalf("events = %+v", events)
	}
}

func TestParseGeminiStreamJSONLineSurfacesFinalResult(t *testing.T) {
	line := `{"type":"result","timestamp":"2026-09-19T13:40:57.045Z","status":"success","stats":{"total_tokens":41255,"tool_calls":2}}`
	events := ParseGeminiStreamJSONLine([]byte(line))
	if len(events) != 1 || events[0].Phase != "result" || events[0].Msg != "done" {
		t.Fatalf("events = %+v", events)
	}
}

func TestParseGeminiStreamJSONLineIgnoresANonJSONRetryWarning(t *testing.T) {
	// gemini-cli prints a plain-text line like this straight to stdout on a
	// transient 503 — confirmed against a real run, not assumed.
	line := `Attempt 1 failed with status 503. Retrying with backoff... _ApiError: {"error":{"message":"..."}}`
	if events := ParseGeminiStreamJSONLine([]byte(line)); events != nil {
		t.Errorf("expected no events for a non-JSON noise line, got %+v", events)
	}
}
