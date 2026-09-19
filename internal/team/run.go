package team

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/redbotster/nanobots/internal/foundry"
)

// Engine selects which coding-agent CLI a Team member runs. Claude Code is
// what context/TEAM-LAB-DESIGN.md names as the first Team harness; Gemini
// CLI is a second, added and verified against a real invocation the same
// way the first one was (a real file-read task, not guessed from either
// tool's docs) — see docs/team.md for what that verification found.
type Engine string

const (
	EngineClaude Engine = "claude"
	EngineGemini Engine = "gemini"
)

// Config is what Run needs that doesn't change task to task.
type Config struct {
	RepoRoot        string
	TeamDir         string // persistent workspaces live under here, e.g. ~/.nanobots/team
	AnthropicAPIKey string // needed for EngineClaude
	GeminiAPIKey    string // needed for EngineGemini
}

// Run gives one Team member one task, streaming its progress on events —
// the same live-progress shape a foundry job already streams into a run's
// log, reused rather than reinvented (see internal/foundry.Event).
func Run(ctx context.Context, cfg Config, in TaskInput, events chan<- foundry.Event) error {
	if in.Role == "" {
		return fmt.Errorf("a Team task needs a role")
	}
	if in.Task == "" {
		return fmt.Errorf("a Team task needs a task")
	}
	engine := in.Engine
	if engine == "" {
		engine = EngineClaude
	}

	switch engine {
	case EngineClaude:
		if cfg.AnthropicAPIKey == "" {
			return fmt.Errorf("ANTHROPIC_API_KEY isn't set in ~/.secrets/nanobots.env — the claude engine needs its own key, for the same reason the foundry's coding agent does (see internal/foundry.ClaudeCLIAgent's doc comment): Shroud is a single-shot proxy, not a multi-turn tool-using session")
		}
	case EngineGemini:
		if cfg.GeminiAPIKey == "" {
			return fmt.Errorf("GEMINI_API_KEY isn't set in ~/.secrets/nanobots.env — the gemini engine needs its own key, for the same reason the claude engine needs ANTHROPIC_API_KEY")
		}
	default:
		return fmt.Errorf("unknown engine %q — must be %q or %q", engine, EngineClaude, EngineGemini)
	}

	workDir, err := EnsureWorkspace(cfg.RepoRoot, cfg.TeamDir, in.Role)
	if err != nil {
		return fmt.Errorf("prepare %s's workspace: %w", in.Role, err)
	}

	roleDir := filepath.Join(cfg.TeamDir, in.Role)
	hostBinPath := filepath.Join(roleDir, "bin", "nanobots")
	if err := foundry.BuildScopedBinary(workDir, hostBinPath); err != nil {
		return fmt.Errorf("build a role-scoped nanobots binary: %w", err)
	}
	const containerBinPath = "/usr/local/bin/nanobots"

	brief := BuildBrief(in, containerBinPath)

	if engine == EngineGemini {
		image, err := EnsureGeminiCLIImage(cfg.RepoRoot)
		if err != nil {
			return fmt.Errorf("prepare the gemini sandbox image: %w", err)
		}
		spec := foundry.DockerAgentSpec{
			Image: image,
			// The prompt is an argument, not stdin: gemini-cli's -p takes
			// the prompt value directly and only appends stdin to it,
			// unlike claude -p which reads an unspecified prompt from
			// stdin — confirmed against a real invocation, not assumed
			// from the two tools looking alike.
			Args: []string{
				"-p", brief,
				"-o", "stream-json",
				"--approval-mode", "yolo",
				"--skip-trust",
			},
			Env: map[string]string{"GEMINI_API_KEY": cfg.GeminiAPIKey},
			Mounts: []foundry.DockerMount{
				{HostPath: workDir, ContainerPath: "/workspace"},
				{HostPath: hostBinPath, ContainerPath: containerBinPath, ReadOnly: true},
			},
		}
		return foundry.RunDockerAgent(ctx, spec, "", events, ParseGeminiStreamJSONLine)
	}

	image, err := foundry.EnsureClaudeCodeImage(cfg.RepoRoot)
	if err != nil {
		return fmt.Errorf("prepare the sandbox image: %w", err)
	}
	spec := foundry.DockerAgentSpec{
		Image: image,
		Args: []string{
			"-p", "--output-format", "stream-json", "--verbose",
			"--permission-mode", "acceptEdits", "--permission-prompts", "none",
			"--disallowedTools", strings.Join(foundry.DisallowedTools, ","),
		},
		Env: map[string]string{"ANTHROPIC_API_KEY": cfg.AnthropicAPIKey},
		Mounts: []foundry.DockerMount{
			{HostPath: workDir, ContainerPath: "/workspace"},
			{HostPath: hostBinPath, ContainerPath: containerBinPath, ReadOnly: true},
		},
	}
	return foundry.RunDockerAgent(ctx, spec, brief, events, foundry.ParseStreamJSONLine)
}
