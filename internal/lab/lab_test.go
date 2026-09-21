package lab

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/redbotster/nanobots/internal/schema"
	"github.com/redbotster/nanobots/internal/team"
)

// fakeGenerator answers with canned text, the same stand-in
// internal/api/compose_test.go uses for a direct provider — the point
// being that a Session doesn't need a real Shroud client to be tested.
type fakeGenerator struct {
	answers []string
}

func (f *fakeGenerator) Describe() string { return "fake" }

func (f *fakeGenerator) Generate(_ context.Context, _ string, _ schema.Model) (string, error) {
	if len(f.answers) == 0 {
		return "", fmt.Errorf("no more canned answers")
	}
	out := f.answers[0]
	f.answers = f.answers[1:]
	return out, nil
}

func lastLogMsg(s *Session) string {
	entries := s.Run().LogEntries()
	if len(entries) == 0 {
		return ""
	}
	return entries[len(entries)-1].Msg
}

func TestHandleMessageWithNoModelSaysSo(t *testing.T) {
	s := NewSession(nil, Config{})
	s.HandleMessage(context.Background(), "hello")
	if !strings.Contains(lastLogMsg(s), "don't have a model configured") {
		t.Errorf("last log line = %q, want it to name the missing model", lastLogMsg(s))
	}
}

func TestHandleMessageAnswer(t *testing.T) {
	gen := &fakeGenerator{answers: []string{`{"action":"answer","text":"Hi! What would you like to build?"}`}}
	s := NewSession(gen, Config{})
	s.HandleMessage(context.Background(), "hello")
	if got := lastLogMsg(s); got != "Hi! What would you like to build?" {
		t.Errorf("last log line = %q", got)
	}
}

func TestHandleMessageWithAnUnparseableDecisionSaysSo(t *testing.T) {
	gen := &fakeGenerator{answers: []string{"not json at all"}}
	s := NewSession(gen, Config{})
	s.HandleMessage(context.Background(), "hello")
	if !strings.Contains(lastLogMsg(s), "couldn't decide") {
		t.Errorf("last log line = %q, want it to admit the decision failed", lastLogMsg(s))
	}
}

func TestHandleMessageStatusOnARoleWithNoWorkspace(t *testing.T) {
	gen := &fakeGenerator{answers: []string{`{"action":"status","role":"nobody"}`}}
	s := NewSession(gen, Config{Team: team.Config{TeamDir: t.TempDir()}})
	s.HandleMessage(context.Background(), "what has nobody done?")
	if !strings.Contains(lastLogMsg(s), "no workspace yet") {
		t.Errorf("last log line = %q", lastLogMsg(s))
	}
}

func TestHandleMessageStatusReportsRealCommits(t *testing.T) {
	repo := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run("init", "-q")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "test")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run("add", "README.md")
	run("commit", "-q", "-m", "initial")

	teamDir := t.TempDir()
	if _, err := team.EnsureWorkspace(repo, teamDir, "designer"); err != nil {
		t.Fatalf("EnsureWorkspace: %v", err)
	}
	workDir := filepath.Join(teamDir, "designer", "workspace")
	if err := os.WriteFile(filepath.Join(workDir, "notes.txt"), []byte("did a thing"), 0o600); err != nil {
		t.Fatal(err)
	}
	commit := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", workDir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	commit("add", "notes.txt")
	commit("commit", "-q", "-m", "designer did a thing")

	gen := &fakeGenerator{answers: []string{`{"action":"status","role":"designer"}`}}
	s := NewSession(gen, Config{Team: team.Config{RepoRoot: repo, TeamDir: teamDir}})
	s.HandleMessage(context.Background(), "what has designer done?")
	if !strings.Contains(lastLogMsg(s), "designer did a thing") {
		t.Errorf("last log line = %q, want the real commit message in it", lastLogMsg(s))
	}
}

func TestHandleMessageDelegateWithNoEngineConfiguredSaysSo(t *testing.T) {
	gen := &fakeGenerator{answers: []string{`{"action":"delegate","role":"backend-engineer","task":"add a bot"}`}}
	s := NewSession(gen, Config{Team: team.Config{TeamDir: t.TempDir()}})
	s.HandleMessage(context.Background(), "add a stripe bot")
	if !strings.Contains(lastLogMsg(s), "no Team engine is configured") {
		t.Errorf("last log line = %q", lastLogMsg(s))
	}
}

func TestHandleMessageAutomateWithNoComposerConfiguredSaysSo(t *testing.T) {
	gen := &fakeGenerator{answers: []string{`{"action":"automate","request":"summarise my inbox every morning"}`}}
	s := NewSession(gen, Config{})
	s.HandleMessage(context.Background(), "set up a basic automation")
	if !strings.Contains(lastLogMsg(s), "composer needs a model") {
		t.Errorf("last log line = %q, want it to admit the composer isn't wired up", lastLogMsg(s))
	}
}

func TestHandleMessageAutomateReportsAComposeError(t *testing.T) {
	gen := &fakeGenerator{answers: []string{`{"action":"automate","request":"summarise my inbox"}`}}
	s := NewSession(gen, Config{Automate: func(context.Context, string) (AutomateResult, error) {
		return AutomateResult{}, fmt.Errorf("the model timed out")
	}})
	s.HandleMessage(context.Background(), "set up a basic automation")
	if !strings.Contains(lastLogMsg(s), "couldn't build that") || !strings.Contains(lastLogMsg(s), "timed out") {
		t.Errorf("last log line = %q, want the real error surfaced", lastLogMsg(s))
	}
}

func TestHandleMessageAutomateReportsAGap(t *testing.T) {
	gen := &fakeGenerator{answers: []string{`{"action":"automate","request":"trade stocks for me"}`}}
	s := NewSession(gen, Config{Automate: func(context.Context, string) (AutomateResult, error) {
		return AutomateResult{Gap: "no bot can place a trade"}, nil
	}})
	s.HandleMessage(context.Background(), "set up a basic automation")
	if !strings.Contains(lastLogMsg(s), "no bot can place a trade") {
		t.Errorf("last log line = %q, want the gap explained", lastLogMsg(s))
	}
}

// Auto-save, don't auto-run: a successful automate call must link to the
// saved swarm (so the WebUI can render something to click, see
// runner.Run.LogOpenSwarm) without ever implying it already ran.
func TestHandleMessageAutomateSuccessLinksToTheSavedSwarm(t *testing.T) {
	gen := &fakeGenerator{answers: []string{`{"action":"automate","request":"summarise my inbox every morning"}`}}
	s := NewSession(gen, Config{Automate: func(context.Context, string) (AutomateResult, error) {
		return AutomateResult{Name: "Inbox Summary", Path: "examples/swarms/inbox-summary.yaml", PlanOK: true}, nil
	}})
	s.HandleMessage(context.Background(), "set up a basic automation")

	entries := s.Run().LogEntries()
	last := entries[len(entries)-1]
	if last.OpenSwarmPath != "examples/swarms/inbox-summary.yaml" {
		t.Errorf("OpenSwarmPath = %q", last.OpenSwarmPath)
	}
	if strings.Contains(last.Msg, "it ran") || strings.Contains(last.Msg, "is running") {
		t.Errorf("message must not claim it ran: %q", last.Msg)
	}
}

// A draft saved with a snap that doesn't type-check yet must say so, not
// claim success — the same "save a work in progress, but don't lie about
// it" policy handleSaveSwarm already has for a human's Save click.
func TestHandleMessageAutomateSuccessWithAPlanErrorSaysSo(t *testing.T) {
	gen := &fakeGenerator{answers: []string{`{"action":"automate","request":"summarise my inbox"}`}}
	s := NewSession(gen, Config{Automate: func(context.Context, string) (AutomateResult, error) {
		return AutomateResult{
			Name: "Inbox Summary", Path: "examples/swarms/inbox-summary.yaml",
			PlanOK: false, PlanError: "notify.message wants a string, got json",
		}, nil
	}})
	s.HandleMessage(context.Background(), "set up a basic automation")
	if !strings.Contains(lastLogMsg(s), "notify.message wants a string") {
		t.Errorf("last log line = %q, want the plan error named", lastLogMsg(s))
	}
}

// Mirrors TestDelegatingLogsAnInterimStepDistinctFromTheFinalAnswer: the
// WebUI's busy indicator relies on an interim step being present and
// distinct from the final, step-less entry.
func TestAutomatingLogsAnInterimStepDistinctFromTheFinalAnswer(t *testing.T) {
	gen := &fakeGenerator{answers: []string{`{"action":"automate","request":"summarise my inbox every morning"}`}}
	s := NewSession(gen, Config{Automate: func(context.Context, string) (AutomateResult, error) {
		return AutomateResult{Name: "Inbox Summary", Path: "examples/swarms/inbox-summary.yaml", PlanOK: true}, nil
	}})
	s.HandleMessage(context.Background(), "set up a basic automation")

	var sawInterim, sawFinal bool
	for _, e := range s.Run().LogEntries() {
		if e.Bot != "lab" {
			continue
		}
		if e.Step == "composing" {
			sawInterim = true
		}
		if e.Step == "" {
			sawFinal = true
		}
	}
	if !sawInterim {
		t.Error("no interim \"composing\" step logged")
	}
	if !sawFinal {
		t.Error("no final, step-less \"lab\" entry logged")
	}
}

// The WebUI tells "Lab is still working" apart from "Lab has answered" by
// whether a "lab"-authored entry carries a step (see LabPage.tsx) — found
// live, after treating any "lab" entry as "answered" cleared the busy
// indicator on this interim line instead of the real one. A regression
// here would silently break that distinction without any Go test failing
// to say so, since nothing else reads Step.
func TestDelegatingLogsAnInterimStepDistinctFromTheFinalAnswer(t *testing.T) {
	prefs, err := team.NewPreferences(filepath.Join(t.TempDir(), "team-engines.json"), team.EngineClaude)
	if err != nil {
		t.Fatal(err)
	}
	gen := &fakeGenerator{answers: []string{`{"action":"delegate","role":"backend-engineer","task":"add a bot"}`}}
	s := NewSession(gen, Config{
		Team:    team.Config{RepoRoot: t.TempDir(), TeamDir: t.TempDir(), AnthropicAPIKey: "sk-test"},
		Engines: prefs,
	})
	s.HandleMessage(context.Background(), "add a stripe bot")

	var sawInterim, sawFinal bool
	for _, e := range s.Run().LogEntries() {
		if e.Bot != "lab" {
			continue
		}
		if e.Step == "delegating" {
			sawInterim = true
		}
		if e.Step == "" {
			sawFinal = true
		}
	}
	if !sawInterim {
		t.Error("no interim \"delegating\" step logged")
	}
	if !sawFinal {
		t.Error("no final, step-less \"lab\" entry logged")
	}
}
