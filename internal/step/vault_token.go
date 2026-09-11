package step

import (
	"fmt"
	"sync"

	"github.com/redbotster/nanobots/internal/oneclaw"
)

// VaultTokenConfig names a static secret in a 1Claw vault — a Slack bot
// token or GitHub personal access token, unlike Google's OAuth refresh
// token, never expires, so there's nothing to refresh, only to fetch once
// and cache.
type VaultTokenConfig struct {
	VaultID string
	Key     string
}

func (cfg VaultTokenConfig) configured() bool { return cfg.VaultID != "" && cfg.Key != "" }

// vaultToken fetches cfg's secret from the vault at most once per process —
// callers needing a fresh read (e.g. after the user rotates a token) restart
// nanobotd, matching how a Google access token cache is scoped to one
// LiveDeps' lifetime too.
type vaultToken struct {
	oc  *oneclaw.Client
	cfg VaultTokenConfig

	mu      sync.Mutex
	value   string
	fetched bool
}

func (v *vaultToken) Get() (string, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.fetched {
		return v.value, nil
	}
	val, err := v.oc.GetSecret(v.cfg.VaultID, v.cfg.Key)
	if err != nil {
		return "", fmt.Errorf("no connected account yet (%w) — connect it from Settings", err)
	}
	v.value = val
	v.fetched = true
	return v.value, nil
}
