package foundry

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ClaudeCLIAgent runs Claude Code (the `claude` CLI) as the coding agent
// that authors a new bot — see agent.go's package doc for why this runs
// inside harness/foundry-agent's container rather than as a raw host
// subprocess restricted by CLI permission flags (tested and confirmed
// those flags don't reliably restrict a tool-using agent's shell access;
// the container boundary is the real, enforced sandbox here).
//
// Requires a real ANTHROPIC_API_KEY, passed through to the container as
// its only credential — a genuinely new prerequisite, not something this
// build provisions. It's a separate credential from whatever this
// machine's own `claude` CLI login uses interactively (that login is
// OAuth/keychain-based, which a container can't share), and separate from
// 1Claw/Shroud (see job.go's package doc on why Shroud can't back this
// either). APIKey is resolved once at daemon startup via the same
// LoadEnvValue(~/.secrets/nanobots.env) every other credential in this
// build goes through (see internal/oneclaw.LoadEnvValue) — not read lazily
// from the process environment here, so it works the same way regardless
// of how nanobotd itself gets launched.
type ClaudeCLIAgent struct {
	RepoRoot string
	APIKey   string
}

// DisallowedTools strips out everything a sandboxed coding agent has no
// legitimate use for — confirmed (unlike --allowedTools' scoped-command
// patterns, which turned out not to restrict anything) that
// --disallowedTools genuinely blocks a tool outright. This is defense in
// depth on top of the container boundary, not the primary safety
// mechanism, and isn't claimed to be an exhaustive list forever — just
// everything observed in this build's own tool surface beyond
// Read/Write/Edit/Bash. Shared with internal/team rather than copied, so
// the two can't quietly drift apart on what a sandboxed agent may not do.
var DisallowedTools = []string{
	"Task", "CronCreate", "CronDelete", "CronList", "DesignSync",
	"EnterWorktree", "ExitWorktree", "ListAgents", "NotebookEdit",
	"ReportFindings", "ScheduleWakeup", "SendMessage", "Skill",
	"TaskOutput", "TaskStop", "ToolSearch", "WebFetch", "WebSearch", "Workflow",
}

func (a *ClaudeCLIAgent) Run(ctx context.Context, workDir string, in BriefInput, events chan<- Event) error {
	if a.APIKey == "" {
		return fmt.Errorf("ANTHROPIC_API_KEY isn't set in ~/.secrets/nanobots.env — the foundry's sandboxed coding agent needs its own key (see docs/foundry.md); this is separate from any interactive `claude` login on this machine, which a container can't share")
	}
	apiKey := a.APIKey

	image, err := EnsureClaudeCodeImage(a.RepoRoot)
	if err != nil {
		return fmt.Errorf("prepare the foundry's sandbox image: %w", err)
	}

	jobDir := filepath.Dir(workDir) // workDir is <jobDir>/worktree
	hostBinPath := filepath.Join(jobDir, "bin", "nanobots")
	if err := BuildScopedBinary(workDir, hostBinPath); err != nil {
		return fmt.Errorf("build a job-scoped nanobots binary: %w", err)
	}
	const containerBinPath = "/usr/local/bin/nanobots"

	brief, err := BuildBrief(a.RepoRoot, in, containerBinPath)
	if err != nil {
		return fmt.Errorf("build the agent's brief: %w", err)
	}

	spec := DockerAgentSpec{
		Image: image,
		Args: []string{
			"-p", "--output-format", "stream-json", "--verbose",
			"--permission-mode", "acceptEdits", "--permission-prompts", "none",
			"--disallowedTools", strings.Join(DisallowedTools, ","),
		},
		Env: map[string]string{"ANTHROPIC_API_KEY": apiKey},
		Mounts: []DockerMount{
			{HostPath: workDir, ContainerPath: "/workspace"},
			{HostPath: hostBinPath, ContainerPath: containerBinPath, ReadOnly: true},
		},
	}
	return RunDockerAgent(ctx, spec, brief, events, ParseStreamJSONLine)
}

// BuildScopedBinary builds a `nanobots` binary from worktreeDir's own
// source (a sandboxed worktree, not the host's possibly-stale bin/nanobots)
// into a location outside the worktree, so it never shows up in a
// git-status check of the worktree itself — foundry's own verifySandbox is
// one such check; internal/team's is another.
func BuildScopedBinary(worktreeDir, outPath string) error {
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return err
	}
	cmd := exec.Command("go", "build", "-o", outPath, "./cmd/nanobots")
	cmd.Dir = worktreeDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// ParseStreamJSONLine turns one line of `claude --output-format
// stream-json --verbose` output into zero or more Events — a single line
// can carry several content blocks (e.g. thinking + tool_use together),
// so this returns a slice rather than assuming one event per line. Shapes
// confirmed against a real invocation, not guessed: assistant
// text/tool_use blocks, user tool_result blocks, and the final result
// line are surfaced; thinking blocks and system/rate_limit_event noise
// are dropped. Exported for internal/team, which runs the identical CLI
// invocation shape against a persistent workspace instead of a job's
// worktree.
func ParseStreamJSONLine(line []byte) []Event {
	var envelope struct {
		Type    string `json:"type"`
		IsError bool   `json:"is_error"`
		Result  string `json:"result"`
		Message struct {
			Content []json.RawMessage `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal(line, &envelope); err != nil {
		return nil
	}

	switch envelope.Type {
	case "assistant":
		var out []Event
		for _, raw := range envelope.Message.Content {
			var block struct {
				Type  string          `json:"type"`
				Text  string          `json:"text"`
				Name  string          `json:"name"`
				Input json.RawMessage `json:"input"`
			}
			if err := json.Unmarshal(raw, &block); err != nil {
				continue
			}
			switch block.Type {
			case "text":
				if strings.TrimSpace(block.Text) != "" {
					out = append(out, Event{Phase: "text", Msg: truncateMsg(block.Text)})
				}
			case "tool_use":
				out = append(out, Event{Phase: "tool", Msg: describeToolUse(block.Name, block.Input)})
			}
			// "thinking" and anything else: no event for this block.
		}
		return out
	case "user":
		var out []Event
		for _, raw := range envelope.Message.Content {
			var block struct {
				Type    string          `json:"type"`
				Content json.RawMessage `json:"content"`
				IsError bool            `json:"is_error"`
			}
			if err := json.Unmarshal(raw, &block); err != nil {
				continue
			}
			if block.Type == "tool_result" {
				out = append(out, Event{Phase: "tool", Msg: describeToolResult(block.Content, block.IsError)})
			}
		}
		return out
	case "result":
		if envelope.IsError {
			return []Event{{Phase: "result", Msg: "failed: " + truncateMsg(envelope.Result)}}
		}
		return []Event{{Phase: "result", Msg: truncateMsg(envelope.Result)}}
	default:
		return nil // system/rate_limit_event/etc — noise
	}
}

// describeToolUse turns one tool_use block into a short, human-readable
// line for the live log — a person watching a foundry job work should see
// "writing bots/new-bot/nanobot.yaml", not a raw JSON tool-call. The one
// exception is the conform-detection prefix ("conform" at the very start
// of the string), which orchestrate.go's drainEvents matches on for
// iteration counting — every branch below preserves it exactly.
func describeToolUse(name string, rawInput json.RawMessage) string {
	if name == "Bash" {
		var in struct {
			Command     string `json:"command"`
			Description string `json:"description"`
		}
		json.Unmarshal(rawInput, &in)
		if strings.Contains(in.Command, "conform") {
			return "conform: checking whether the new bot passes its own contract…"
		}
		if in.Description != "" {
			return "running: " + truncateMsg(in.Description)
		}
		return "running: " + truncateMsg(in.Command)
	}
	var in struct {
		FilePath string `json:"file_path"`
	}
	json.Unmarshal(rawInput, &in)
	path := strings.TrimPrefix(in.FilePath, "/workspace/")
	switch name {
	case "Write":
		if path != "" {
			return "writing " + path
		}
	case "Edit":
		if path != "" {
			return "editing " + path
		}
	case "Read":
		if path != "" {
			return "reading " + path
		}
	}
	if path != "" {
		return name + " " + path
	}
	return name
}

func describeToolResult(raw json.RawMessage, isError bool) string {
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		s = string(raw) // some tool results are structured, not a plain string
	}
	if isError {
		return "error: " + truncateMsg(s)
	}
	return truncateMsg(s)
}

func truncateMsg(s string) string {
	const max = 400
	s = strings.TrimSpace(s)
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}
