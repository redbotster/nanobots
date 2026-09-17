package main

import (
	"strings"
	"testing"

	"github.com/redbotster/nanobots/internal/agentname"
	"github.com/redbotster/nanobots/internal/oneclaw"
	"github.com/redbotster/nanobots/internal/runner"
	"github.com/redbotster/nanobots/internal/schema"
)

// What --prune deletes, and what it must never delete.
//
// This is the one list in the repo whose mistakes cannot be undone by
// anyone: 1Claw shows an agent's api_key exactly once, so a deleted agent
// cannot be recreated as itself. The first version of this classified by
// "is it in the bot catalog", which put the live nanobots-composer and
// nanobots-shroud-proxy agents in the delete pile.
func TestClassifyAgents(t *testing.T) {
	agent := func(name string) oneclaw.Agent { return oneclaw.Agent{ID: "id-" + name, Name: name} }

	wanted := map[string]bool{}
	for _, n := range agentname.WellKnown() {
		wanted[n] = true
	}
	wanted["nanobots-redact-e7ecfa"] = true

	live, stale, theirs := classifyAgents([]oneclaw.Agent{
		agent(agentname.Composer),
		agent(agentname.ShroudProxy),
		agent(agentname.Approvals),
		agent(agentname.Foundry),
		agent("nanobots-redact-e7ecfa"),
		agent("nanobots-inbox-triage"), // the old per-bot scheme
		agent("nanobots-tone"),
		agent("my-agent-agent"), // somebody else's
		agent("prod-worker"),
	}, wanted)

	names := func(as []oneclaw.Agent) string {
		var out []string
		for _, a := range as {
			out = append(out, a.Name)
		}
		return strings.Join(out, ",")
	}

	const wantLive = "nanobots,nanobots-composer,nanobots-foundry,nanobots-redact-e7ecfa,nanobots-shroud-proxy"
	if got := names(live); got != wantLive {
		t.Errorf("live = %q, want %q", got, wantLive)
	}
	if got, want := names(stale), "nanobots-inbox-triage,nanobots-tone"; got != want {
		t.Errorf("stale = %q, want %q", got, want)
	}
	if got, want := names(theirs), "my-agent-agent,prod-worker"; got != want {
		t.Errorf("not ours = %q, want %q", got, want)
	}
}

// Every fixed agent name this repo creates has to be registered in
// internal/agentname, or `agents --prune` offers to delete it.
//
// Asserted rather than trusted, because the failure is silent at the point
// it is introduced — you add an EnsureAgent call, everything works, and the
// damage happens months later when someone tidies up.
func TestEveryWellKnownAgentIsRegistered(t *testing.T) {
	for _, name := range agentname.WellKnown() {
		if !strings.HasPrefix(name, agentname.Prefix) {
			t.Errorf("%q does not start with %q, so it would be treated as somebody else's agent",
				name, agentname.Prefix)
		}
	}
	// The catalog's own agents are derived, not listed — but they must
	// carry the prefix too, for the same reason.
	nb := &schema.Nanobot{}
	nb.Metadata.Name = "probe"
	nb.Spec.Steps = []schema.Step{{Name: "write", Type: "ai.generate"}}
	if got := runner.AgentNameFor(nb); !strings.HasPrefix(got, agentname.Prefix) {
		t.Errorf("a catalog agent is named %q, outside the %q prefix", got, agentname.Prefix)
	}
}

func TestAgentsRejectsUnknownFlags(t *testing.T) {
	if err := runAgents([]string{"--delete-everything"}); err == nil {
		t.Error("an unknown flag should be refused rather than ignored")
	}
}
