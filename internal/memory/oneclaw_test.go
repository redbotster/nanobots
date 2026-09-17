package memory

import (
	"context"
	"testing"

	"github.com/redbotster/nanobots/internal/oneclaw"
)

// The 1Claw backend must not advertise recall, and the reason is a live
// probe rather than a reading of the docs.
//
// 1Claw exposes POST /v1/agents/{id}/memory/search with a top_k and a score
// per result. It is in the OpenAPI spec; the prose docs say memory search
// does not exist; the spec is usually the one to trust, and this repo has
// been caught twice believing the prose. So it was probed:
//
//	PUT   .../memory/probe-ns/obs-live-1 "refunds always get escalated" -> stored
//	GET   .../memory/probe-ns            -> the entry, tier "durable"
//	search {"query":"refunds"}                          -> 0 results
//	search {"query":"refunds always get escalated"}     -> 0 results
//	search {"query":""}                                 -> 1 result
//
// An empty query returns everything, any non-empty one returns nothing, and
// 60 seconds of waiting changes neither. The route answers; the matching
// does not work.
//
// Implementing Recaller on top of that would give every bot with a
// memory.recall step a permanent, silent "nothing known" — worse than
// ErrNoRecall, which at least says the backend cannot do it. This test
// exists so that stays a decision rather than an oversight.
func TestTheOneClawBackendDoesNotClaimRecall(t *testing.T) {
	var store Store = &OneClaw{Client: oneclaw.NewClient("k"), AgentID: "a"}
	if _, ok := RecallerOf(store); ok {
		t.Fatal("the 1Claw backend advertises recall — see docs/1claw-feature-requests.md #12 " +
			"and re-probe /v1/agents/{id}/memory/search before enabling this")
	}

	// And the failure a bot sees is the explanatory one.
	if _, err := Recall(context.Background(), store, "ns", "what is urgent?"); err == nil {
		t.Error("recall against a key/value backend succeeded")
	}
}
