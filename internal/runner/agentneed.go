package runner

import (
	"strings"

	"github.com/redbotster/nanobots/internal/schema"
)

// nativeProviders are the services LiveDeps calls through a dedicated client
// of this build's own (internal/google, internal/slack, …). Everything else
// goes through 1Claw's generic binding, which is addressed by agent id — so
// only a live service outside this set forces an agent to exist.
//
// Keep in sync with LiveDeps.ServiceCall's dispatch in internal/step/live.go.
var nativeProviders = map[string]bool{
	"google":   true,
	"github":   true,
	"slack":    true,
	"stripe":   true,
	"hubspot":  true,
	"x":        true,
	"linkedin": true,
}

// needsOneClawAgent reports whether this bot actually requires a 1Claw agent
// of its own.
//
// It used to be "always", which meant a purely deterministic bot — render-pdf,
// drive-save, notify, post-publisher and six others, a third of the catalog —
// burned one of the account's ten agent slots to never use it. That is what
// made a fourteen-bot workspace impossible on a pro tier, and it also cost
// every such bot an agent-creation round-trip on its first run.
//
// A bot needs an agent for exactly four reasons:
//   - ai.generate, which is proxied through that agent's Shroud credentials;
//   - agent.loop, for the same reason — GenerateWithTools is Shroud too,
//     and a bot whose only model-calling step is a loop otherwise never got
//     an agent at all, so its ai.generate-shaped call silently fell back to
//     a direct provider key with no tool-calling and failed immediately
//     with llm.ErrNoToolCalling. Found running lead-enricher@0.2.0 for
//     real, not by a unit test — DemoDeps never reaches this function;
//   - memory.*, which is namespaced per agent;
//   - a live (non-demo) service with no native client here, which reaches the
//     provider through 1Claw's generic binding, addressed by agent id.
//
// Approvals are not on this list, and that is now a design decision rather
// than a limitation. An `approve` step does open a copy of the question in
// 1Claw's own queue so it can be answered away from this machine — but it
// does that as one shared agent (`runner.ApprovalAgentName`), not as the
// bot. Giving four approving bots an agent each would spend four capped
// slots to change whose name is on a question addressed to you.
//
// The comment here used to say the mirror "simply never succeeds", and
// before that that it never ran at all. Both were true in sequence and
// neither is now: see RunQueueApprover.mirror for the exchange that was
// missed.
func needsOneClawAgent(nb *schema.Nanobot) bool {
	for _, s := range nb.Spec.Steps {
		if s.Type == "ai.generate" || s.Type == "agent.loop" || strings.HasPrefix(s.Type, "memory.") {
			return true
		}
	}
	for _, svc := range nb.Spec.Services {
		live := svc.Connection != schema.ConnectionDemo && svc.Connection != ""
		if live && !nativeProviders[svc.Provider] {
			return true
		}
	}
	return false
}
