package secrets

import (
	"errors"
	"fmt"
	"time"

	"github.com/zalando/go-keyring"
)

// keychainService namespaces every secret this package puts in the OS
// keychain from anything else on the machine using the same library.
const keychainService = "nanobots-secrets"

// Keychain stores secrets in the OS's own credential store — Keychain on
// macOS (via the `security` CLI, not cgo), Secret Service on Linux, Windows
// Credential Manager. No file for anyone to copy, no key for this package
// to manage — the OS already solved that problem.
//
// It does not always work. Verified live in this build's own sandboxed
// dev shell: `keyring.Set` returned exit status 36, and the `security` CLI
// underneath it gave the real reason directly —
// "SecKeychainItemCreateFromContent (<default>): User interaction is not
// allowed." A non-interactive session (CI, a container, a sandboxed
// shell — exactly the environment nanobotd itself might run in) cannot
// unlock or write to a login keychain at all. That is not a bug in this
// wrapper; it is what the OS is supposed to do. Available is the honest
// answer to "will this work right now", checked with a real write/delete,
// not assumed from the platform name — callers (internal/wiring's backend
// selection, /api/status) must call it before relying on this backend
// rather than treating "compiled for macOS" as "works on this machine".
type Keychain struct{}

func (k Keychain) Describe() string {
	return "your OS keychain (macOS Keychain, Secret Service, or Windows Credential Manager) — " +
		"nothing else on this machine can read it without the same access you have"
}

// availabilityProbeTimeout bounds how long Available will wait for the OS
// to answer. Measured live in this repo's own dev machine: the fast path —
// a sandboxed, non-interactive shell refusing outright — returns in well
// under a second ("User interaction is not allowed"). But a session that
// *can* show UI can instead pop a real, silent permission dialog and then
// wait for a human to click it, forever: run inside `go test`, that exact
// probe hung for the full ten-minute test timeout before this bound
// existed. A caller (nanobotd's own startup, in particular) must never
// hang on this, so a timeout is not an optimization here, it is the fix
// for a real, reproduced incident — and it fails closed (not available)
// rather than assuming success, matching what "we couldn't confirm this
// works" should mean everywhere else in this codebase.
const availabilityProbeTimeout = 2 * time.Second

// Available does a real, throwaway set-then-delete rather than trusting
// the platform name, because the platform name is exactly what makes this
// look available when it is not: this build compiles the macOS backend on
// every macOS host, whether or not the current session can actually pass
// its "User interaction is not allowed" gate. Bounded by
// availabilityProbeTimeout — see its comment for why that bound exists.
//
// The probe goroutine is intentionally leaked on a timeout: go-keyring
// gives no way to cancel an in-flight `security` call, and a leaked
// goroutine that eventually finishes (or never does, pinned on a dialog no
// one will click) is a far smaller cost than the caller hanging with it.
func Available() bool {
	done := make(chan bool, 1)
	go func() {
		const probeUser = "nanobots-availability-probe"
		if err := keyring.Set(keychainService, probeUser, "probe"); err != nil {
			done <- false
			return
		}
		_ = keyring.Delete(keychainService, probeUser)
		done <- true
	}()
	select {
	case ok := <-done:
		return ok
	case <-time.After(availabilityProbeTimeout):
		return false
	}
}

func (k Keychain) Get(key string) (string, bool, error) {
	v, err := keyring.Get(keychainService, key)
	if err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("secrets: keychain get %q: %w", key, err)
	}
	return v, true, nil
}

func (k Keychain) Put(key, value string) error {
	if err := keyring.Set(keychainService, key, value); err != nil {
		return fmt.Errorf("secrets: keychain set %q: %w", key, err)
	}
	return nil
}

func (k Keychain) Delete(key string) error {
	if err := keyring.Delete(keychainService, key); err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return nil
		}
		return fmt.Errorf("secrets: keychain delete %q: %w", key, err)
	}
	return nil
}

var _ Store = Keychain{}
