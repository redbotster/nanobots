package foundry

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/redbotster/nanobots/internal/schema"
)

// TestClaudeCLIAgentAuthorsARealBot is the real end-to-end proof: a real
// ANTHROPIC_API_KEY, a real Docker container, a real Claude Code session
// authoring an actual conforming bot from scratch. Skipped unless both
// NANOBOTS_LIVE_TEST=1 and ANTHROPIC_API_KEY are set, the same convention
// internal/oneclaw/live_test.go uses for anything that spends real money
// or needs a real credential this build can't provision — see
// docs/connections.md.
func TestClaudeCLIAgentAuthorsARealBot(t *testing.T) {
	if os.Getenv("NANOBOTS_LIVE_TEST") == "" {
		t.Skip("set NANOBOTS_LIVE_TEST=1 to run this against a real coding agent")
	}
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		t.Skip("no ANTHROPIC_API_KEY configured")
	}
	repoRoot := repoRootForTest(t)
	botsDir := t.TempDir()

	o := &Orchestrator{Config{
		RepoRoot: repoRoot, BotsDir: botsDir, WorkDir: t.TempDir(),
		Agent:        &ClaudeCLIAgent{RepoRoot: repoRoot, APIKey: os.Getenv("ANTHROPIC_API_KEY")},
		MaxWallClock: 10 * time.Minute,
	}}

	job, err := o.StartJob(
		"give me ten post ideas about my niche",
		"brainstorm short-form content post ideas for a given niche",
		[]schema.InputPort{{Name: "niche", Type: "string", Required: true}},
		[]schema.OutputPort{{Name: "ideas", Type: "list<json>"}},
	)
	if err != nil {
		t.Fatalf("StartJob: %v", err)
	}

	deadline := time.Now().Add(15 * time.Minute)
	for time.Now().Before(deadline) {
		if job.Outcome() != "" {
			break
		}
		if pending := job.PendingApprovals(); len(pending) > 0 {
			t.Logf("bot authored: %s (iterations=%d)", job.BotID(), job.Iterations())
			if err := job.Decide(pending[0].ID, true, "live-test"); err != nil {
				t.Fatalf("Decide: %v", err)
			}
		}
		time.Sleep(2 * time.Second)
	}

	if job.Outcome() != OutcomePromoted {
		t.Fatalf("Outcome = %q (want promoted); log:\n%v", job.Outcome(), job.LogEntries())
	}
	t.Logf("promoted bots/%s after %d conform iterations", job.BotID(), job.Iterations())
}

// TestEnsureFoundryAgentImageBuilds is a lighter live check that doesn't
// need an API key — just that Docker itself is available and the image
// builds and runs the CLI. Skipped unless NANOBOTS_LIVE_TEST=1 (it does a
// real docker build, slower than this package's other tests).
func TestEnsureFoundryAgentImageBuilds(t *testing.T) {
	if os.Getenv("NANOBOTS_LIVE_TEST") == "" {
		t.Skip("set NANOBOTS_LIVE_TEST=1 to build the real foundry-agent image")
	}
	root := repoRootForTest(t)
	image, err := ensureFoundryAgentImage(root)
	if err != nil {
		t.Fatalf("ensureFoundryAgentImage: %v", err)
	}

	events := make(chan Event, 16)
	go func() {
		for range events {
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	err = runDockerAgent(ctx, dockerAgentSpec{Image: image, Args: []string{"--version"}}, "", events, func([]byte) []Event { return nil })
	close(events)
	if err != nil {
		t.Fatalf("run --version in the foundry-agent image: %v", err)
	}
}
