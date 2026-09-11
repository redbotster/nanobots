package foundry

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCreateAndRemoveWorktree(t *testing.T) {
	repoRoot, _ := newTestRepo(t)
	worktreeDir := filepath.Join(t.TempDir(), "worktree")

	if err := createWorktree(repoRoot, worktreeDir, "foundry/test-1"); err != nil {
		t.Fatalf("createWorktree: %v", err)
	}
	if _, err := os.Stat(filepath.Join(worktreeDir, "bots", "existing-bot", "nanobot.yaml")); err != nil {
		t.Fatalf("worktree missing the repo's own tracked files: %v", err)
	}

	if err := removeWorktree(repoRoot, worktreeDir, "foundry/test-1"); err != nil {
		t.Fatalf("removeWorktree: %v", err)
	}
	if _, err := os.Stat(worktreeDir); !os.IsNotExist(err) {
		t.Errorf("expected worktree dir to be gone, stat err = %v", err)
	}
}

func TestExistingBotIDs(t *testing.T) {
	_, botsDir := newTestRepo(t)
	ids, err := existingBotIDs(botsDir)
	if err != nil {
		t.Fatalf("existingBotIDs: %v", err)
	}
	if !ids["existing-bot"] || len(ids) != 1 {
		t.Errorf("ids = %v", ids)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestVerifySandboxOnlyTouchedOneNewBot(t *testing.T) {
	existing := map[string]bool{"existing-bot": true}

	t.Run("clean single new bot", func(t *testing.T) {
		repoRoot, _ := newTestRepo(t)
		worktreeDir := filepath.Join(t.TempDir(), "wt")
		if err := createWorktree(repoRoot, worktreeDir, "foundry/a"); err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(worktreeDir, "bots", "new-bot", "nanobot.yaml"), "x: 1")
		writeFile(t, filepath.Join(worktreeDir, "bots", "new-bot", "fixtures", "inputs.json"), "{}")

		botID, err := verifySandbox(worktreeDir, existing)
		if err != nil {
			t.Fatalf("verifySandbox: %v", err)
		}
		if botID != "new-bot" {
			t.Errorf("botID = %q", botID)
		}
	})

	t.Run("file outside bots rejected", func(t *testing.T) {
		repoRoot, _ := newTestRepo(t)
		worktreeDir := filepath.Join(t.TempDir(), "wt")
		if err := createWorktree(repoRoot, worktreeDir, "foundry/b"); err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(worktreeDir, "bots", "new-bot", "nanobot.yaml"), "x: 1")
		writeFile(t, filepath.Join(worktreeDir, "internal", "sneaky.go"), "package main")

		if _, err := verifySandbox(worktreeDir, existing); err == nil {
			t.Fatal("expected an error for a file touched outside bots/")
		}
	})

	t.Run("two new bot dirs rejected", func(t *testing.T) {
		repoRoot, _ := newTestRepo(t)
		worktreeDir := filepath.Join(t.TempDir(), "wt")
		if err := createWorktree(repoRoot, worktreeDir, "foundry/c"); err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(worktreeDir, "bots", "bot-one", "nanobot.yaml"), "x: 1")
		writeFile(t, filepath.Join(worktreeDir, "bots", "bot-two", "nanobot.yaml"), "x: 1")

		if _, err := verifySandbox(worktreeDir, existing); err == nil {
			t.Fatal("expected an error for two different bot directories touched")
		}
	})

	t.Run("touched existing catalog id rejected", func(t *testing.T) {
		repoRoot, botsDir := newTestRepo(t)
		worktreeDir := filepath.Join(t.TempDir(), "wt")
		if err := createWorktree(repoRoot, worktreeDir, "foundry/d"); err != nil {
			t.Fatal(err)
		}
		ids, err := existingBotIDs(botsDir)
		if err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(worktreeDir, "bots", "existing-bot", "nanobot.yaml"), "changed: true")

		if _, err := verifySandbox(worktreeDir, ids); err == nil {
			t.Fatal("expected an error for touching an existing catalog bot")
		}
	})

	t.Run("no changes at all rejected", func(t *testing.T) {
		repoRoot, _ := newTestRepo(t)
		worktreeDir := filepath.Join(t.TempDir(), "wt")
		if err := createWorktree(repoRoot, worktreeDir, "foundry/e"); err != nil {
			t.Fatal(err)
		}
		if _, err := verifySandbox(worktreeDir, existing); err == nil {
			t.Fatal("expected an error when nothing was touched")
		}
	})
}
