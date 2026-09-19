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

func TestRunRefusesWithNoAnthropicAPIKey(t *testing.T) {
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

func TestRunRefusesWithNoGeminiAPIKey(t *testing.T) {
	events := make(chan foundry.Event, 8)
	err := Run(context.Background(), Config{RepoRoot: t.TempDir(), TeamDir: t.TempDir()},
		TaskInput{Role: "designer", Task: "anything", Engine: EngineGemini}, events)
	if err == nil {
		t.Fatal("expected an error with no GEMINI_API_KEY")
	}
	if !strings.Contains(err.Error(), "GEMINI_API_KEY") {
		t.Errorf("error doesn't name the missing key: %v", err)
	}
}

func TestRunRefusesAnUnknownEngine(t *testing.T) {
	events := make(chan foundry.Event, 8)
	err := Run(context.Background(), Config{RepoRoot: t.TempDir(), TeamDir: t.TempDir(), AnthropicAPIKey: "sk-test", GeminiAPIKey: "g-test"},
		TaskInput{Role: "designer", Task: "anything", Engine: "gpt4"}, events)
	if err == nil {
		t.Fatal("expected an error for an unknown engine")
	}
}

func TestRunRefusesWithNoRole(t *testing.T) {
	events := make(chan foundry.Event, 8)
	err := Run(context.Background(), Config{RepoRoot: t.TempDir(), TeamDir: t.TempDir(), AnthropicAPIKey: "sk-test"},
		TaskInput{Task: "anything"}, events)
	if err == nil {
		t.Fatal("expected an error with no role")
	}
}

func TestRunRefusesWithNoTask(t *testing.T) {
	events := make(chan foundry.Event, 8)
	err := Run(context.Background(), Config{RepoRoot: t.TempDir(), TeamDir: t.TempDir(), AnthropicAPIKey: "sk-test"},
		TaskInput{Role: "designer"}, events)
	if err == nil {
		t.Fatal("expected an error with no task")
	}
}

// TestTeamRunGivesARealTaskToARealAgent is the real end-to-end proof, the
// same convention internal/foundry/agent_claude_live_test.go and
// internal/oneclaw/live_test.go use for anything that needs a real
// credential this build can't provision — skipped unless
// NANOBOTS_LIVE_TEST=1 and that engine's own key are both set. Both
// engines are exercised because both were verified against a real
// invocation, not just the first one this package shipped with.
func TestTeamRunGivesARealTaskToARealAgent(t *testing.T) {
	if os.Getenv("NANOBOTS_LIVE_TEST") == "" {
		t.Skip("set NANOBOTS_LIVE_TEST=1 to run this against a real coding agent")
	}
	repoRoot := repoRootForTeamTest(t)

	for _, tc := range []struct {
		engine Engine
		envKey string
	}{
		{EngineClaude, "ANTHROPIC_API_KEY"},
		{EngineGemini, "GEMINI_API_KEY"},
	} {
		t.Run(string(tc.engine), func(t *testing.T) {
			key := os.Getenv(tc.envKey)
			if key == "" {
				t.Skipf("no %s configured", tc.envKey)
			}
			teamDir := t.TempDir()
			role := "test-live-run-" + string(tc.engine)
			// This test creates a real git worktree and branch against the
			// actual repo it's run from (EnsureWorkspace has nowhere else to
			// put one) — cleaned up here so a live-tested run doesn't leave
			// team/test-live-run-* behind on the machine that ran it.
			t.Cleanup(func() {
				workDir := filepath.Join(teamDir, role, "workspace")
				exec.Command("git", "-C", repoRoot, "worktree", "remove", "--force", workDir).Run()
				exec.Command("git", "-C", repoRoot, "branch", "-D", "team/"+role).Run()
			})

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()

			cfg := Config{RepoRoot: repoRoot, TeamDir: teamDir}
			switch tc.engine {
			case EngineClaude:
				cfg.AnthropicAPIKey = key
			case EngineGemini:
				cfg.GeminiAPIKey = key
			}

			events := make(chan foundry.Event, 256)
			done := make(chan error, 1)
			go func() {
				done <- Run(ctx, cfg,
					TaskInput{
						Role:   role,
						Engine: tc.engine,
						Task:   "Read docs/anatomy.md and reply with one sentence describing what a nanobot is. Do not write or edit any file.",
					},
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
		})
	}
}
