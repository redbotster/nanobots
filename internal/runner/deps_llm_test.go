package runner

import (
	"context"
	"strings"
	"testing"

	"github.com/redbotster/nanobots/internal/llm"
	"github.com/redbotster/nanobots/internal/schema"
	"github.com/redbotster/nanobots/internal/step"
)

func probeBot() *schema.Nanobot {
	return &schema.Nanobot{
		Metadata:   schema.Metadata{Name: "probe", Version: "0.1.0"},
		SourcePath: "testdata/probe",
		Spec: schema.NanobotSpec{
			Model: schema.Model{Provider: "anthropic", Name: "claude-sonnet-4-6"},
		},
	}
}

func buildWith(t *testing.T, run *Run, gen llm.Generator) step.Deps {
	t.Helper()
	return BuildDeps(run, "probe", probeBot(), nil, "", "", nil,
		step.GoogleConfig{}, step.GitHubConfig{}, step.SlackConfig{}, step.StripeConfig{},
		step.HubSpotConfig{}, step.XConfig{}, step.LinkedInConfig{}, nil, nil, gen)
}

// A provider key with no 1Claw used to get you nothing: BuildDeps returned
// pure DemoDeps unless 1Claw was configured, so every ai.generate in the
// catalog returned canned fixture text while a perfectly good
// ANTHROPIC_API_KEY sat unused, with nothing in the log saying so.
func TestAnLLMAloneIsEnoughToRunLive(t *testing.T) {
	run := NewRun("probe-swarm")
	deps := buildWith(t, run, &recordingGenerator{answer: "from the real model"})

	if _, ok := deps.(*step.LiveDeps); !ok {
		t.Fatalf("got %T, want LiveDeps — an LLM key alone should mean live", deps)
	}
	out, err := deps.AIGenerate("hello", schema.Model{Provider: "anthropic", Name: "claude-sonnet-4-6"})
	if err != nil {
		t.Fatal(err)
	}
	if out != "from the real model" {
		t.Errorf("out = %q — the prompt did not reach the generator", out)
	}
}

// Neither 1Claw nor an LLM is still a legitimate install (a fresh clone),
// and must stay on fixtures rather than failing.
func TestNoOneClawAndNoLLMStaysOnFixtures(t *testing.T) {
	deps := buildWith(t, NewRun("probe-swarm"), nil)
	if _, ok := deps.(*step.DemoDeps); !ok {
		t.Fatalf("got %T, want DemoDeps", deps)
	}
}

// A bot declaring anthropic served by a Gemini key gets a different model.
// That is the right behaviour — one key should run the whole catalog — but
// it has to land in the run log, where whoever reads the run will see it.
func TestAModelSubstitutionReachesTheRunLog(t *testing.T) {
	run := NewRun("probe-swarm")
	gen := llm.NewGemini("k", "gemini-3.5-flash-lite")
	// Never dialled: the substitution happens before the request.
	deps := buildWith(t, run, gen)

	_, _ = deps.AIGenerate("hello", schema.Model{Provider: "anthropic", Name: "claude-sonnet-4-6"})

	var found bool
	for _, l := range run.LogEntries() {
		if strings.Contains(l.Msg, "claude-sonnet-4-6") && strings.Contains(l.Msg, "gemini-3.5-flash-lite") {
			found = true
		}
	}
	if !found {
		t.Errorf("the substitution never reached the run log; entries were %+v", run.LogEntries())
	}
}

type recordingGenerator struct{ answer string }

func (r *recordingGenerator) Describe() string { return "test" }
func (r *recordingGenerator) Generate(context.Context, string, schema.Model) (string, error) {
	return r.answer, nil
}
