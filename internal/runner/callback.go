package runner

import (
	"fmt"
	"sync"

	"github.com/redbotster/nanobots/internal/step"
)

// CallbackRegistry maps a run's per-bot random token to the step.Deps that
// token is allowed to invoke. internal/api's /internal/steps/* handlers look
// tokens up here — a token means nothing beyond "this one container, this
// one bot instance, this one run" (see cmd/nanobot-agent and
// internal/step.RemoteDeps for why the container itself holds nothing more
// sensitive than this).
type CallbackRegistry struct {
	mu    sync.RWMutex
	byTok map[string]step.Deps
}

func NewCallbackRegistry() *CallbackRegistry {
	return &CallbackRegistry{byTok: map[string]step.Deps{}}
}

func (c *CallbackRegistry) Register(token string, deps step.Deps) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.byTok[token] = deps
}

func (c *CallbackRegistry) Unregister(token string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.byTok, token)
}

func (c *CallbackRegistry) Lookup(token string) (step.Deps, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	d, ok := c.byTok[token]
	if !ok {
		return nil, fmt.Errorf("unknown or expired run token")
	}
	return d, nil
}
