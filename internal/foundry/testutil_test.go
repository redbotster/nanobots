package foundry

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// newTestRepo creates a throwaway git repo (with one commit, so `worktree
// add ... HEAD` has something to branch from) containing a bots/ dir with
// one pre-existing bot directory, for collision-id testing. Returns the
// repo root and its bots dir.
func newTestRepo(t *testing.T) (repoRoot, botsDir string) {
	t.Helper()
	repoRoot = t.TempDir()
	botsDir = filepath.Join(repoRoot, "bots")
	if err := os.MkdirAll(filepath.Join(botsDir, "existing-bot"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(botsDir, "existing-bot", "nanobot.yaml"), []byte("placeholder: true\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repoRoot
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run("init", "-q")
	run("-c", "user.name=test", "-c", "user.email=test@test.local", "add", "-A")
	run("-c", "user.name=test", "-c", "user.email=test@test.local", "commit", "-q", "-m", "init")
	return repoRoot, botsDir
}
