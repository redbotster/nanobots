package team

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/redbotster/nanobots/internal/foundry"
)

// Config is what Run needs that doesn't change task to task.
type Config struct {
	RepoRoot string
	TeamDir  string // persistent workspaces live under here, e.g. ~/.nanobots/team
	APIKey   string // ANTHROPIC_API_KEY
}

// Run gives one Team member one task, streaming its progress on events —
// the same live-progress shape a foundry job already streams into a run's
// log, reused rather than reinvented (see internal/foundry.Event).
func Run(ctx context.Context, cfg Config, in TaskInput, events chan<- foundry.Event) error {
	if cfg.APIKey == "" {
		return fmt.Errorf("ANTHROPIC_API_KEY isn't set in ~/.secrets/nanobots.env — a Team member needs its own key, for the same reason the foundry's coding agent does (see internal/foundry.ClaudeCLIAgent's doc comment): Shroud is a single-shot proxy, not a multi-turn tool-using session")
	}
	if in.Role == "" {
		return fmt.Errorf("a Team task needs a role")
	}
	if in.Task == "" {
		return fmt.Errorf("a Team task needs a task")
	}

	workDir, err := EnsureWorkspace(cfg.RepoRoot, cfg.TeamDir, in.Role)
	if err != nil {
		return fmt.Errorf("prepare %s's workspace: %w", in.Role, err)
	}

	image, err := foundry.EnsureClaudeCodeImage(cfg.RepoRoot)
	if err != nil {
		return fmt.Errorf("prepare the sandbox image: %w", err)
	}

	roleDir := filepath.Join(cfg.TeamDir, in.Role)
	hostBinPath := filepath.Join(roleDir, "bin", "nanobots")
	if err := foundry.BuildScopedBinary(workDir, hostBinPath); err != nil {
		return fmt.Errorf("build a role-scoped nanobots binary: %w", err)
	}
	const containerBinPath = "/usr/local/bin/nanobots"

	brief := BuildBrief(in, containerBinPath)

	spec := foundry.DockerAgentSpec{
		Image: image,
		Args: []string{
			"-p", "--output-format", "stream-json", "--verbose",
			"--permission-mode", "acceptEdits", "--permission-prompts", "none",
			"--disallowedTools", strings.Join(foundry.DisallowedTools, ","),
		},
		Env: map[string]string{"ANTHROPIC_API_KEY": cfg.APIKey},
		Mounts: []foundry.DockerMount{
			{HostPath: workDir, ContainerPath: "/workspace"},
			{HostPath: hostBinPath, ContainerPath: containerBinPath, ReadOnly: true},
		},
	}
	return foundry.RunDockerAgent(ctx, spec, brief, events, foundry.ParseStreamJSONLine)
}
