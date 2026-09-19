package team

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/redbotster/nanobots/internal/foundry"
)

func repoRootForTeamTest(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Join(filepath.Dir(file), "..", "..")
	if _, err := os.Stat(filepath.Join(root, "bots")); err != nil {
		t.Fatalf("wrong repo root %s: %v", root, err)
	}
	return root
}

func TestRunRefusesWithNoAPIKey(t *testing.T) {
	events := make(chan foundry.Event, 8)
	err := Run(context.Background(), Config{RepoRoot: t.TempDir(), TeamDir: t.TempDir()},
		TaskInput{Role: "designer", Task: "anything"}, events)
	if err == nil {
		t.Fatal("expected an error with no ANTHROPIC_API_KEY")
	}
	if !strings.Contains(err.Error(), "ANTHROPIC_API_KEY") {
		t.Errorf("error doesn't name the missing key: %v", err)
	}
}

func TestRunRefusesWithNoRole(t *testing.T) {
	events := make(chan foundry.Event, 8)
	err := Run(context.Background(), Config{RepoRoot: t.TempDir(), TeamDir: t.TempDir(), APIKey: "sk-test"},
		TaskInput{Task: "anything"}, events)
	if err == nil {
		t.Fatal("expected an error with no role")
	}
}

func TestRunRefusesWithNoTask(t *testing.T) {
	events := make(chan foundry.Event, 8)
	err := Run(context.Background(), Config{RepoRoot: t.TempDir(), TeamDir: t.TempDir(), APIKey: "sk-test"},
		TaskInput{Role: "designer"}, events)
	if err == nil {
		t.Fatal("expected an error with no task")
	}
}

// TestTeamRunGivesARealTaskToARealAgent is the real end-to-end proof, the
// same convention internal/foundry/agent_claude_live_test.go and
// internal/oneclaw/live_test.go use for anything that needs a real
// credential this build can't provision — skipped unless both
// NANOBOTS_LIVE_TEST=1 and ANTHROPIC_API_KEY are set.
func TestTeamRunGivesARealTaskToARealAgent(t *testing.T) {
	if os.Getenv("NANOBOTS_LIVE_TEST") == "" {
		t.Skip("set NANOBOTS_LIVE_TEST=1 to run this against a real coding agent")
	}
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("no ANTHROPIC_API_KEY configured")
	}
	repoRoot := repoRootForTeamTest(t)
	teamDir := t.TempDir()
	const role = "test-live-run"
	// This test creates a real git worktree and branch against the actual
	// repo it's run from (EnsureWorkspace has nowhere else to put one) —
	// cleaned up here so a live-tested run doesn't leave team/test-live-run
	// behind on the machine that ran it.
	t.Cleanup(func() {
		workDir := filepath.Join(teamDir, role, "workspace")
		exec.Command("git", "-C", repoRoot, "worktree", "remove", "--force", workDir).Run()
		exec.Command("git", "-C", repoRoot, "branch", "-D", "team/"+role).Run()
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	events := make(chan foundry.Event, 256)
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, Config{RepoRoot: repoRoot, TeamDir: teamDir, APIKey: apiKey},
			TaskInput{Role: role, Task: "Read docs/anatomy.md and reply with one sentence describing what a nanobot is. Do not write or edit any file."},
			events)
	}()

	var lines []string
	for {
		select {
		case ev := <-events:
			lines = append(lines, ev.Msg)
		case err := <-done:
			if err != nil {
				t.Fatalf("team.Run: %v\ntranscript:\n%s", err, strings.Join(lines, "\n"))
			}
			if len(lines) == 0 {
				t.Fatal("no events streamed from a real run")
			}
			return
		}
	}
}
