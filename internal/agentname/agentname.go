// Package agentname holds every 1Claw agent name this repo creates.
//
// It exists because `nanobots agents --prune` deletes agents, and deciding
// what is unused from a list written by hand is how you delete something
// that was in use. The first version of that command offered to delete
// `nanobots-composer` and `nanobots-shroud-proxy` — both live, both holding
// an api_key 1Claw shows exactly once, so both unrecoverable.
//
// So the names live here, one place, and both the code that creates them and
// the code that decides what is stale read the same list. A new agent that
// is not registered here is one `nanobots agents` will offer to delete,
// which is the failure this package is shaped to make loud rather than
// quiet: add the constant in the same commit as the EnsureAgent call.
//
// Per-bot agents are not here. They are derived from a bot's guardrails
// (runner.AgentNameFor) and there is no fixed list of them by design.
package agentname

// Prefix is what every agent this repo creates starts with. An agent without
// it was made by someone else and is never a candidate for deletion.
const Prefix = "nanobots"

const (
	// Approvals opens the 1Claw copy of every approval gate, and is the
	// agent `nanobots deploy 1claw` runs a hosted instance as.
	Approvals = "nanobots"
	// Composer turns a plain-English request into a draft swarm.
	Composer = "nanobots-composer"
	// ShroudProxy backs the local /shroud/v1 proxy.
	ShroudProxy = "nanobots-shroud-proxy"
	// Foundry authors a new bot when the catalog genuinely cannot do it.
	Foundry = "nanobots-foundry"
)

// WellKnown is every fixed name, for the code that has to tell a live agent
// from a leftover one.
func WellKnown() []string {
	return []string{Approvals, Composer, ShroudProxy, Foundry}
}
