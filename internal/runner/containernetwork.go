package runner

import "github.com/redbotster/nanobots/internal/schema"

// localSteps reach nothing outside the container: they read the inputs
// already mounted at /run and write to /run/outputs. Everything else either
// calls back to nanobotd (service.call, ai.generate, memory.*, approve,
// notify, web.fetch) or is a step type this list has not been taught about.
//
// A deny-by-default list rather than an allow-by-default one, because the
// cost of the two mistakes is not symmetrical: a new step type wrongly
// treated as local would have its network pulled out from under it and fail
// loudly on the first run, while one wrongly treated as remote just keeps
// the bridge it has today.
var localSteps = map[string]bool{
	"transform.render": true,
	"transform.now":    true,
	"transform.pick":   true,
	"stop.if":          true,
}

// needsContainerNetwork reports whether this bot's container has any reason
// to have a network interface at all.
//
// `guardrails.network_egress` has always been reported per bot and never
// enforced in the container — the honest note has been in docs and in
// dockerRunArgs for as long as both existed. Enforcing an *allowlist* needs
// a per-run network and an egress proxy, which is a real project. Enforcing
// *none* is a Docker flag, and it is the whole of the declared policy for a
// bot that declares no egress and calls nothing.
//
// `render-pdf` is that bot today: one transform.render, no services, no
// model, no memory. It renders HTML someone else produced, and with no
// interface its Chromium cannot fetch a remote asset either — which is
// exactly the hole the TODO in docker.go describes, closed for the one bot
// where closing it costs nothing.
func needsContainerNetwork(nb *schema.Nanobot) bool {
	// A bot that says where it may go is a bot that intends to go
	// somewhere. Taking its network away because its steps look local
	// would contradict its own declaration.
	if len(nb.Spec.Guardrails.NetworkEgress) > 0 {
		return true
	}
	// A declared service is a callback waiting to happen, even if no step
	// uses it today.
	if len(nb.Spec.Services) > 0 {
		return true
	}
	for _, s := range nb.Spec.Steps {
		if !localSteps[s.Type] {
			return true
		}
	}
	return false
}
