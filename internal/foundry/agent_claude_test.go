package foundry

import (
	"context"
	"strings"
	"testing"
)

// These are real lines captured from an actual `claude -p --output-format
// stream-json --verbose` invocation (see agent_claude.go's doc comment) —
// not hand-guessed shapes.

func TestParseStreamJSONLineSurfacesAssistantText(t *testing.T) {
	line := `{"type":"assistant","message":{"model":"claude-sonnet-5","id":"msg_1","type":"message","role":"assistant","content":[{"type":"text","text":"Hey there, friend!"}]},"parent_tool_use_id":null,"session_id":"s1","uuid":"u1"}`
	events := ParseStreamJSONLine([]byte(line))
	if len(events) != 1 || events[0].Phase != "text" || events[0].Msg != "Hey there, friend!" {
		t.Fatalf("events = %+v", events)
	}
}

func TestParseStreamJSONLineDropsThinkingBlocks(t *testing.T) {
	line := `{"type":"assistant","message":{"content":[{"type":"thinking","thinking":"...","signature":"abc"}]}}`
	events := ParseStreamJSONLine([]byte(line))
	if len(events) != 0 {
		t.Fatalf("expected no events for a thinking-only block, got %+v", events)
	}
}

func TestParseStreamJSONLineSurfacesBashToolUse(t *testing.T) {
	line := `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"toolu_1","name":"Bash","input":{"command":"cat hello.txt","description":"Print contents of hello.txt"},"caller":{"type":"direct"}}]}}`
	events := ParseStreamJSONLine([]byte(line))
	// Prefers the model's own short human description of the command over
	// the raw shell text, when one was given — more readable for a human
	// watching the log, and Claude Code's Bash tool always supplies one.
	if len(events) != 1 || events[0].Phase != "tool" || events[0].Msg != "running: Print contents of hello.txt" {
		t.Fatalf("events = %+v", events)
	}
}

func TestParseStreamJSONLineFallsBackToRawCommandWithNoDescription(t *testing.T) {
	line := `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"toolu_1b","name":"Bash","input":{"command":"cat hello.txt"}}]}}`
	events := ParseStreamJSONLine([]byte(line))
	if len(events) != 1 || events[0].Msg != "running: cat hello.txt" {
		t.Fatalf("events = %+v", events)
	}
}

func TestParseStreamJSONLineFlagsConformCommandsSpecifically(t *testing.T) {
	line := `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"toolu_2","name":"Bash","input":{"command":"/usr/local/bin/nanobots conform bots/new-bot","description":"check conformance"}}]}}`
	events := ParseStreamJSONLine([]byte(line))
	if len(events) != 1 || events[0].Phase != "tool" {
		t.Fatalf("events = %+v", events)
	}
	if got := events[0].Msg; got[:len("conform")] != "conform" {
		t.Errorf("Msg = %q, expected it to start with %q so the orchestrator's iteration counter recognizes it", got, "conform")
	}
}

func TestParseStreamJSONLineSurfacesWriteToolUseWithFilePath(t *testing.T) {
	line := `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"toolu_3","name":"Write","input":{"file_path":"/workspace/bots/new-bot/nanobot.yaml","content":"..."}}]}}`
	events := ParseStreamJSONLine([]byte(line))
	// Human-readable: a verb, and the container's /workspace mount point
	// stripped since it means nothing to someone watching the log.
	if len(events) != 1 || events[0].Msg != "writing bots/new-bot/nanobot.yaml" {
		t.Fatalf("events = %+v", events)
	}
}

func TestParseStreamJSONLineSurfacesToolResult(t *testing.T) {
	line := `{"type":"user","message":{"content":[{"tool_use_id":"toolu_1","type":"tool_result","content":"hi","is_error":false}]}}`
	events := ParseStreamJSONLine([]byte(line))
	if len(events) != 1 || events[0].Phase != "tool" || events[0].Msg != "hi" {
		t.Fatalf("events = %+v", events)
	}
}

func TestParseStreamJSONLineSurfacesToolResultError(t *testing.T) {
	line := `{"type":"user","message":{"content":[{"tool_use_id":"toolu_2","type":"tool_result","content":"Exit code 127: command not found","is_error":true}]}}`
	events := ParseStreamJSONLine([]byte(line))
	if len(events) != 1 || events[0].Msg != "error: Exit code 127: command not found" {
		t.Fatalf("events = %+v", events)
	}
}

func TestParseStreamJSONLineSurfacesFinalResult(t *testing.T) {
	line := `{"type":"result","subtype":"success","is_error":false,"result":"Created the bot.","duration_ms":2479}`
	events := ParseStreamJSONLine([]byte(line))
	if len(events) != 1 || events[0].Phase != "result" || events[0].Msg != "Created the bot." {
		t.Fatalf("events = %+v", events)
	}
}

func TestParseStreamJSONLineSurfacesFinalErrorResult(t *testing.T) {
	line := `{"type":"result","is_error":true,"result":"Not logged in · Please run /login"}`
	events := ParseStreamJSONLine([]byte(line))
	if len(events) != 1 || events[0].Phase != "result" {
		t.Fatalf("events = %+v", events)
	}
	if got := events[0].Msg; got == "" || got[:len("failed:")] != "failed:" {
		t.Errorf("Msg = %q, expected it to be prefixed with failed:", got)
	}
}

func TestParseStreamJSONLineDropsSystemAndRateLimitNoise(t *testing.T) {
	for _, line := range []string{
		`{"type":"system","subtype":"init","cwd":"/workspace"}`,
		`{"type":"system","subtype":"hook_started","hook_id":"x"}`,
		`{"type":"rate_limit_event","rate_limit_info":{}}`,
	} {
		if events := ParseStreamJSONLine([]byte(line)); len(events) != 0 {
			t.Errorf("line %q: expected no events, got %+v", line, events)
		}
	}
}

func TestParseStreamJSONLineIgnoresUnparsableInput(t *testing.T) {
	if events := ParseStreamJSONLine([]byte("not json")); events != nil {
		t.Errorf("expected nil for unparsable input, got %+v", events)
	}
}

func TestClaudeCLIAgentRunRequiresAPIKey(t *testing.T) {
	agent := &ClaudeCLIAgent{RepoRoot: "/tmp", APIKey: ""}
	err := agent.Run(context.Background(), t.TempDir(), BriefInput{}, make(chan Event, 1))
	if err == nil {
		t.Fatal("expected an error when APIKey is empty")
	}
	if !strings.Contains(err.Error(), "ANTHROPIC_API_KEY") {
		t.Errorf("error = %q, expected it to mention ANTHROPIC_API_KEY", err.Error())
	}
}
