package team

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/redbotster/nanobots/internal/foundry"
)

const geminiCLIImage = "nanobots/team-gemini:local"

// EnsureGeminiCLIImage builds the Gemini CLI sandbox image if it isn't
// already present locally — same build-if-missing pattern as
// foundry.EnsureClaudeCodeImage, a separate image because it's a different
// npm package with a different entrypoint, not because the sandboxing
// story differs (see harness/team-gemini/Dockerfile).
func EnsureGeminiCLIImage(repoRoot string) (string, error) {
	check := exec.Command("docker", "image", "inspect", geminiCLIImage)
	if err := check.Run(); err == nil {
		return geminiCLIImage, nil
	}
	build := exec.Command("docker", "build", "-f", filepath.Join(repoRoot, "harness", "team-gemini", "Dockerfile"), "-t", geminiCLIImage, repoRoot)
	var stderr bytes.Buffer
	build.Stderr = &stderr
	if err := build.Run(); err != nil {
		return "", fmt.Errorf("build %s: %w: %s", geminiCLIImage, err, stderr.String())
	}
	return geminiCLIImage, nil
}

// ParseGeminiStreamJSONLine turns one line of `gemini -o stream-json`
// output into zero or more Events. Shapes confirmed against a real
// invocation (a file-read task, not guessed from docs): "init" is
// dropped, a "message" with role "user" just echoes the prompt back and is
// dropped, an assistant "message" streams as text in a few chunks
// (`"delta":true`, each a fragment of the growing reply), "tool_use" and
// "tool_result" pair the way Claude's "assistant"/"user" tool blocks do,
// and "result" carries the final status with no separate error-message
// field observed — a non-success status is reported from what's there
// rather than assuming a field that hasn't been seen.
//
// A line gemini-cli itself prints that isn't JSON at all — a retry
// warning, a stack trace from a transient 503 — fails json.Unmarshal and
// is dropped here exactly like Claude's system/rate_limit_event noise.
func ParseGeminiStreamJSONLine(line []byte) []foundry.Event {
	var envelope struct {
		Type    string          `json:"type"`
		Role    string          `json:"role"`
		Content string          `json:"content"`
		Tool    string          `json:"tool_name"`
		Params  json.RawMessage `json:"parameters"`
		Status  string          `json:"status"`
		Output  string          `json:"output"`
	}
	if err := json.Unmarshal(line, &envelope); err != nil {
		return nil
	}

	switch envelope.Type {
	case "message":
		if envelope.Role != "assistant" || strings.TrimSpace(envelope.Content) == "" {
			return nil // the "user" message is just the prompt echoed back
		}
		return []foundry.Event{{Phase: "text", Msg: truncateGeminiMsg(envelope.Content)}}
	case "tool_use":
		return []foundry.Event{{Phase: "tool", Msg: describeGeminiToolUse(envelope.Tool, envelope.Params)}}
	case "tool_result":
		if envelope.Status != "success" {
			return []foundry.Event{{Phase: "tool", Msg: "error: " + truncateGeminiMsg(envelope.Output)}}
		}
		return []foundry.Event{{Phase: "tool", Msg: truncateGeminiMsg(envelope.Output)}}
	case "result":
		if envelope.Status != "success" {
			return []foundry.Event{{Phase: "result", Msg: "failed: " + envelope.Status}}
		}
		return []foundry.Event{{Phase: "result", Msg: "done"}}
	default:
		return nil // "init" and anything else observed — noise
	}
}

func describeGeminiToolUse(name string, rawParams json.RawMessage) string {
	var params struct {
		FilePath string `json:"file_path"`
		Command  string `json:"command"`
	}
	json.Unmarshal(rawParams, &params)
	switch {
	case params.FilePath != "":
		return name + " " + params.FilePath
	case params.Command != "":
		return "running: " + truncateGeminiMsg(params.Command)
	default:
		return name
	}
}

func truncateGeminiMsg(s string) string {
	const max = 400
	s = strings.TrimSpace(s)
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}
