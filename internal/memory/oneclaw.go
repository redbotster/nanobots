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
// Key/value only. 1Claw's memory API is a flat namespaced store; it derives
// nothing, so it does not implement Recaller and Recall against it fails
// with ErrNoRecall rather than quietly returning nothing.
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
