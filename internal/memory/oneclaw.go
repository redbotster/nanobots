package memory

import (
	"context"

	"github.com/redbotster/nanobots/internal/oneclaw"
)

// OneClaw is key/value memory on a 1Claw agent — what this build had before
// there was a choice. Kept as one backend among several rather than as the
// only path, so a bot that just wants to remember `last_run_at` no longer
// needs 1Claw configured at all.
//
// Key/value only, and that is a measured conclusion rather than an
// assumption. 1Claw does expose `POST /v1/agents/{id}/memory/search` with a
// `top_k` and a score per result — it is in the OpenAPI spec, and this repo
// spent a while believing the prose docs that say otherwise. Probed against
// the live API before wiring it up:
//
//	PUT   .../memory/probe-ns/obs-live-1  "refunds always get escalated"  -> stored
//	GET   .../memory/probe-ns             -> the entry, tier "durable"
//	POST  .../memory/search  {"query":"refunds"}                  -> 0 results
//	POST  .../memory/search  {"query":"refunds always get escalated"} -> 0 results
//	POST  .../memory/search  {"query":""}                         -> 1 result
//
// An empty query returns everything and any non-empty one returns nothing,
// including the stored text character for character, unchanged after 60
// seconds. The endpoint answers; its matching does not work on this account,
// and nothing in the 499-path spec configures an embedding model.
//
// So this deliberately does not implement Recaller. A Recall that always
// answered "nothing known" would be worse than ErrNoRecall, which at least
// tells a bot the backend cannot do it — see
// docs/1claw-feature-requests.md #12.
type OneClaw struct {
	Client  *oneclaw.Client
	AgentID string
}

func (o *OneClaw) Get(_ context.Context, namespace, key string) (string, bool, error) {
	if err := validKey(namespace, key); err != nil {
		return "", false, err
	}
	return o.Client.MemoryGet(o.AgentID, namespace, key)
}

func (o *OneClaw) Put(_ context.Context, namespace, key, value string) error {
	if err := validKey(namespace, key); err != nil {
		return err
	}
	return o.Client.MemoryPut(o.AgentID, namespace, key, value)
}

var _ Store = (*OneClaw)(nil)
