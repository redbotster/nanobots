package step

import (
	"fmt"
	"sync"

	"github.com/redbotster/nanobots/internal/oneclaw"
	"github.com/redbotster/nanobots/internal/secrets"
)

// VaultTokenConfig names a static secret — a Slack bot token or GitHub
// personal access token, unlike Google's OAuth refresh token, never
// expires, so there's nothing to refresh, only to fetch once and cache.
// It no longer names which vault: the secrets.Store a vaultToken is built
// with already knows where it lives, whether that's a 1Claw vault, the OS
// keychain, or an encrypted local file (see docs/secrets.md).
type VaultTokenConfig struct {
	Key string
}

// vaultToken fetches cfg's secret at most once per process — callers
// needing a fresh read (e.g. after the user rotates a token) restart
// nanobotd, matching how a Google access token cache is scoped to one
// LiveDeps' lifetime too.
type vaultToken struct {
	store secrets.Store
	cfg   VaultTokenConfig

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
	if v.store == nil {
		return "", fmt.Errorf("no secrets backend configured — see docs/secrets.md")
	}
	val, found, err := v.store.Get(v.cfg.Key)
	if err != nil {
		// A locked 1Claw vault is not a missing connection: the credential
		// is almost certainly right there, 1Claw just wants the human to
		// re-verify first. Telling someone to "connect it from Settings"
		// when they already have sends them to do the one thing that
		// won't help.
		if locked, ok := oneclaw.AsVaultLocked(err); ok {
			return "", locked
		}
		return "", fmt.Errorf("reading %q failed: %w", v.cfg.Key, err)
	}
	if !found {
		return "", fmt.Errorf("no connected account yet — connect it from Settings")
	}
	v.value = val
	v.fetched = true
	return v.value, nil
}

// wrapTokenErr turns a vaultToken failure into the provider-specific,
// actionable message each *_live.go client returns — except when the
// failure is a locked vault, which is not "not configured" and gets its
// own, different advice (see vaultToken.Get).
func wrapTokenErr(provider, hint string, err error) error {
	if _, ok := oneclaw.AsVaultLocked(err); ok {
		return err
	}
	return fmt.Errorf("%s: not configured — %s (%w)", provider, hint, err)
}
