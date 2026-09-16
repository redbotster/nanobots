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
// A bot needs an agent for exactly three reasons:
//   - ai.generate, which is proxied through that agent's Shroud credentials;
//   - memory.*, which is namespaced per agent;
//   - a live (non-demo) service with no native client here, which reaches the
//     provider through 1Claw's generic binding, addressed by agent id.
//
// Approvals are not on that list, and the reason is not the one this comment
// used to give. It said "BuildDeps always installs the local
// RunQueueApprover, so an `approve` step never touches 1Claw's own queue",
// which is untrue: the approver does try to mirror the approval into 1Claw
// so it can be answered from a phone. It simply never succeeds. mirror()
// needs an agent id, no approving bot in the catalog qualifies above, and so
// across 108 runs on this machine that opened an approval, not one logged
// either "also asked on 1Claw" or "could not also ask on 1Claw".
//
// Giving approving bots an agent was tried, and the path is blocked further
// up regardless — see RunQueueApprover.mirror for the two credentials and
// the two refusals. So they stay off this list: provisioning four more
// agents to feed a call that cannot succeed would cost real slots for a
// logged error per approval.
func needsOneClawAgent(nb *schema.Nanobot) bool {
	for _, s := range nb.Spec.Steps {
		if s.Type == "ai.generate" || strings.HasPrefix(s.Type, "memory.") {
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
