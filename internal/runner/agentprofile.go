package runner

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/redbotster/nanobots/internal/schema"
)

// Which 1Claw agent a bot runs as.
//
// It used to be one per bot name — `nanobots-inbox-triage`,
// `nanobots-draft-replies`, twenty-seven of them on this account out of a
// plan cap of fifty, with `Agent limit reached` waiting mid-run for anyone
// who added a few more bots. The audit's suggestion was one agent for
// everything, which is cheap and loses something real: an agent *is* the
// guardrail boundary. `shroud_config` — the PII policy, the injection
// threshold, the allowed providers, the daily budget — is set per agent,
// and 1Claw's chat request body carries no per-call override (checked
// against the live OpenAPI: SendChatMessageRequest has no policy fields).
// Collapsing to one agent would silently give every bot one policy.
//
// So agents are keyed by the guardrail profile instead. Two bots that
// declare the same guardrails are the same thing from 1Claw's point of
// view and share an agent; a bot that declares something different gets its
// own, automatically, without anyone maintaining a list.
//
// Measured against the real catalog before choosing: the 27 bots that need
// an agent at all declare exactly two distinct profiles — sixteen
// `pii: redact` and eleven `pii: allow`, everything else identical. So this
// is 27 agents to 2, with nothing given up.

// agentProfile is the canonical form of everything that ends up in a 1Claw
// agent's shroud_config. Two bots with the same profile can share an agent;
// two with different profiles must not.
//
// Built from the same values agentRequestFor sends, in a fixed order, so the
// string is stable across runs and across machines. If a field is ever added
// to the request, it belongs here too — a profile that ignores a real
// difference is how two bots end up quietly sharing the wrong policy.
func agentProfile(nb *schema.Nanobot) string {
	g := nb.Spec.Guardrails
	return fmt.Sprintf("pii=%s;injection=%.4f;providers=%s;budget=%.4f;secrets=true;memory=true",
		orDefault(g.PII, "redact"),
		orDefaultF(g.InjectionThreshold, 0.7),
		nb.Spec.Model.Provider,
		g.DailyBudgetUSD,
	)
}

// agentNameFor is the 1Claw agent name for a bot's guardrail profile.
//
// Readable half first, so the account's agent list says something to a
// human: `nanobots-redact-7c1f9a`, not a bare hash. The suffix is what
// makes it correct — two profiles that differ only in daily budget would
// otherwise collide on the same name and the second bot would run under the
// first one's limits.
func agentNameFor(nb *schema.Nanobot) string {
	sum := sha256.Sum256([]byte(agentProfile(nb)))
	return "nanobots-" + safeLabel(orDefault(nb.Spec.Guardrails.PII, "redact")) +
		"-" + hex.EncodeToString(sum[:])[:6]
}

// agentDescriptionFor is what the 1Claw dashboard shows next to the name, so
// an agent called nanobots-redact-7c1f9a is not a mystery in an account
// someone comes back to in six months.
//
// Deliberately not the system prompt: that field reaches the model on every
// call, and a sentence describing the agent's own configuration is the last
// thing a bot's prompt needs.
func agentDescriptionFor(nb *schema.Nanobot) string {
	g := nb.Spec.Guardrails
	desc := fmt.Sprintf("nanobots: PII %s, injection threshold %.2f, provider %s",
		orDefault(g.PII, "redact"), orDefaultF(g.InjectionThreshold, 0.7), nb.Spec.Model.Provider)
	if g.DailyBudgetUSD > 0 {
		desc += fmt.Sprintf(", daily budget $%.2f", g.DailyBudgetUSD)
	}
	return desc + ". Shared by every bot declaring these guardrails."
}

// safeLabel keeps a yaml-supplied value from shaping an agent name into
// something 1Claw will not take, or something that reads as a different
// agent. A typo'd `pii: Redact!!` becomes `redact`, which is also what
// riskTierNumber-style defaulting does elsewhere: recognisable values
// through, everything else to something safe.
func safeLabel(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "agent"
	}
	if b.Len() > 20 {
		return b.String()[:20]
	}
	return b.String()
}

// AgentNameFor is the 1Claw agent a bot runs as, or "" if it needs none.
//
// Exported for `nanobots agents`, which works out what is still in use by
// asking the catalog the same question the runner asks — so a stale agent
// and a live one can never be told apart by two different rules.
func AgentNameFor(nb *schema.Nanobot) string {
	if !needsOneClawAgent(nb) {
		return ""
	}
	return agentNameFor(nb)
}
