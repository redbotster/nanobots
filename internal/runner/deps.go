package runner

import (
	"github.com/redbotster/nanobots/internal/llm"
	"github.com/redbotster/nanobots/internal/memory"
	"github.com/redbotster/nanobots/internal/oneclaw"
	"github.com/redbotster/nanobots/internal/schema"
	"github.com/redbotster/nanobots/internal/step"
)

// BuildDeps resolves the step.Deps a bot instance runs against for one run:
// LiveDeps (real services and a real LLM, with any `connection: demo`
// service still falling back to fixtures) when this deployment has either
// 1Claw or an LLM provider, and pure DemoDeps when it has neither. Both get
// the same RunQueueApprover so approvals always surface through the run's
// own queue — see approver.go.
//
// "either" matters. This used to be "1Claw or nothing", so a machine with
// an ANTHROPIC_API_KEY and no 1Claw account ran the entire catalog against
// demo fixtures — every ai.generate returning canned text, with nothing in
// the log saying the key was being ignored. An LLM is now enough on its
// own; 1Claw is still what adds real service calls, vault credentials and
// Shroud's budget and redaction guardrails on top.
func BuildDeps(run *Run, botID string, nb *schema.Nanobot, oc *oneclaw.Client, agentID, agentAPIKey string, blobs step.BlobStore, services step.ServiceConfigs, override step.Approver, mem memory.Store, gen llm.Generator) step.Deps {
	fixturesDir := nb.SourcePath + "/fixtures"
	var approver step.Approver = &RunQueueApprover{Run: run, Bot: botID, Step: "approve"}
	if override != nil {
		approver = override
	}

	hasOneClaw := oc != nil && oc.Configured()
	if !hasOneClaw && gen == nil {
		d := step.NewDemoDeps(fixturesDir, blobs)
		d.Approver = approver
		return d
	}
	ld := step.NewLiveDeps(oc, oneclaw.NewShroudClient(agentID, agentAPIKey), agentID, fixturesDir, blobs)
	ld.Approver = approver
	ld.Services = services
	// Every service call this bot makes, marked when it was served from
	// fixtures — see Run.NoteDemoService for why that has to be visible.
	ld.OnServiceCall = func(svc schema.Service, _ string, demo bool) {
		if demo {
			run.NoteDemoService(botID, svc.ID)
		}
	}
	ld.Memory = mem
	ld.LLM = generatorFor(run, botID, gen, oc, agentID, agentAPIKey)
	return ld
}

// generatorFor resolves the run-time LLM for one bot.
//
// The backend is chosen at startup, but a Shroud client is per-agent and a
// bot's agent is only provisioned once the run reaches it — so startup
// leaves a llm.DeferredShroud marker and this swaps in the real thing,
// exactly as memoryFor does for 1Claw agent memory.
//
// It also points a direct backend's substitution notice at the run log. A
// bot declaring anthropic/claude-sonnet-4-6 served by a Gemini key gets a
// different model, and that has to be visible where someone reading the run
// will see it.
func generatorFor(run *Run, botID string, gen llm.Generator, oc *oneclaw.Client, agentID, agentAPIKey string) llm.Generator {
	if gen == nil {
		return nil
	}
	if llm.IsDeferredShroud(gen) {
		if oc != nil && oc.Configured() && agentID != "" && agentAPIKey != "" {
			return llm.NewShroud(oneclaw.NewShroudClient(agentID, agentAPIKey))
		}
		// No agent yet and no 1Claw: fall through to whatever the marker
		// wraps, which is nil when nothing else is configured.
		gen = gen.(*llm.DeferredShroud).Fallback
		if gen == nil {
			return nil
		}
	}
	note := func(msg string) { run.Log(botID, "ai.generate", "%s", msg) }
	switch g := gen.(type) {
	case *llm.Anthropic:
		g.OnSubstitute = note
	case *llm.OpenAI:
		g.OnSubstitute = note
	case *llm.Gemini:
		g.OnSubstitute = note
	}
	return gen
}
