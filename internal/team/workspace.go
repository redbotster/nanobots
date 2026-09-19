// Package team runs a persistent, role-scoped coding-agent worker — the
// "Team" half of context/TEAM-LAB-DESIGN.md's Human -> Lab Agent -> Team
// agents -> nanobots/nanoswarms hierarchy. This is step 2 of that doc's
// own "Proposed next step": exactly one Team harness (Claude Code) against
// exactly one role at a time, no Lab tab yet, proving a Team agent can
// propose a real change without a shortcut around the ordinary approval
// gate — before anything is built on top of it.
//
// Reuses internal/foundry's sandbox wholesale (the same image, the same
// container runner, the same stream-json parser, the same disallowed-tools
// list) rather than building a second one: a Team member and a foundry job
// are the same kind of thing — a coding-agent CLI, confined by a container
// boundary, given a git worktree to work in — differing only in whether
// that worktree is thrown away after one task or kept.
package team

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// DefaultTeamDir returns ~/.nanobots/team, creating it if needed — outside
// any git tree, the same reasoning oneclaw.DefaultStateDir already applies
// to credentials: a role's workspace lives under the repo (it's a
// worktree, git needs that), but where nanobots itself tracks "which
// roles exist" does not.
func DefaultTeamDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".nanobots", "team")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

// EnsureWorkspace returns role's persistent worktree, creating it on its
// own branch ("team/<role>") the first time and reusing it on every call
// after. Unlike a foundry job's worktree — created fresh, removed on
// success, kept only for forensic inspection on failure — a Team member's
// workspace is meant to accumulate history across many tasks, the same
// way a person's own working copy would: its git log is part of what the
// next task's agent has to go on.
func EnsureWorkspace(repoRoot, teamDir, role string) (string, error) {
	worktreeDir := filepath.Join(teamDir, role, "workspace")
	if _, err := os.Stat(worktreeDir); err == nil {
		return worktreeDir, nil
	} else if !os.IsNotExist(err) {
		return "", err
	}

	if err := os.MkdirAll(filepath.Dir(worktreeDir), 0o755); err != nil {
		return "", err
	}

	branch := "team/" + role
	args := []string{"-C", repoRoot, "worktree", "add", worktreeDir}
	if exec.Command("git", "-C", repoRoot, "rev-parse", "--verify", branch).Run() == nil {
		// The branch survived a worktree directory that didn't (someone
		// cleaned up by hand, or a prior run only got partway) — reuse it
		// rather than starting the role over from HEAD.
		args = append(args, branch)
	} else {
		args = append(args, "-b", branch, "HEAD")
	}
	cmd := exec.Command("git", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("git worktree add: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return worktreeDir, nil
}

// Roles lists every role that has ever been given a workspace, by
// scanning teamDir — the only source of truth this needs, since a role
// exists exactly when EnsureWorkspace has been called for it once.
func Roles(teamDir string) ([]string, error) {
	entries, err := os.ReadDir(teamDir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var roles []string
	for _, e := range entries {
		if e.IsDir() {
			roles = append(roles, e.Name())
		}
	}
	return roles, nil
}
