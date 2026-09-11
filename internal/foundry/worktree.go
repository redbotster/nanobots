package foundry

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func createWorktree(repoRoot, worktreeDir, branch string) error {
	if err := os.MkdirAll(filepath.Dir(worktreeDir), 0o755); err != nil {
		return err
	}
	cmd := exec.Command("git", "-C", repoRoot, "worktree", "add", "-b", branch, worktreeDir, "HEAD")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git worktree add: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// removeWorktree tears a job's sandbox all the way down — only ever called
// after a successful promote (see promote.go); a rejected or failed job's
// worktree/branch is deliberately left on disk for forensic inspection.
func removeWorktree(repoRoot, worktreeDir, branch string) error {
	cmd := exec.Command("git", "-C", repoRoot, "worktree", "remove", "--force", worktreeDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git worktree remove: %w: %s", err, strings.TrimSpace(string(out)))
	}
	cmd = exec.Command("git", "-C", repoRoot, "branch", "-D", branch)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git branch -D: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// existingBotIDs lists the current catalog's top-level bot directory names
// — used both for the brief's collision-avoidance hint and verifySandbox's
// "didn't touch an existing bot" check.
func existingBotIDs(botsDir string) (map[string]bool, error) {
	entries, err := os.ReadDir(botsDir)
	if err != nil {
		return nil, err
	}
	ids := make(map[string]bool, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			ids[e.Name()] = true
		}
	}
	return ids, nil
}

// verifySandbox is a second, independently-implemented check beyond the
// worktree's own OS-level directory boundary — it parses git's own status
// output (a completely different mechanism than "which directory did the
// subprocess write into") and requires every changed/added/renamed path to
// sit under exactly one bots/<id>/ directory that isn't already a catalog
// id. Returns the one new bot id on success. Deliberately conservative: any
// ambiguity (an unparseable line, a path outside bots/, more than one bot
// touched) fails closed rather than guessing.
func verifySandbox(worktreeDir string, existingIDs map[string]bool) (string, error) {
	cmd := exec.Command("git", "-C", worktreeDir, "status", "--porcelain", "--untracked-files=all")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git status: %w", err)
	}

	touched := map[string]bool{}
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	for sc.Scan() {
		line := sc.Text()
		if len(line) < 4 {
			continue
		}
		rest := line[3:] // porcelain v1: two status chars + one space, then the path(s)
		paths := []string{rest}
		if idx := strings.Index(rest, " -> "); idx >= 0 { // a rename: "old -> new" — both sides count
			paths = []string{rest[:idx], rest[idx+len(" -> "):]}
		}
		for _, p := range paths {
			p = strings.Trim(p, `"`)
			if !strings.HasPrefix(p, "bots/") {
				return "", fmt.Errorf("sandbox violation: touched %q, outside bots/", p)
			}
			parts := strings.SplitN(strings.TrimPrefix(p, "bots/"), "/", 2)
			if len(parts) == 0 || parts[0] == "" {
				return "", fmt.Errorf("sandbox violation: touched %q directly under bots/, not inside a bot directory", p)
			}
			touched[parts[0]] = true
		}
	}

	if len(touched) == 0 {
		return "", fmt.Errorf("the agent made no changes under bots/")
	}
	if len(touched) > 1 {
		names := make([]string, 0, len(touched))
		for n := range touched {
			names = append(names, n)
		}
		return "", fmt.Errorf("sandbox violation: touched more than one bot directory: %s", strings.Join(names, ", "))
	}
	var botID string
	for n := range touched {
		botID = n
	}
	if existingIDs[botID] {
		return "", fmt.Errorf("sandbox violation: touched an existing catalog bot %q instead of creating a new one", botID)
	}
	return botID, nil
}
