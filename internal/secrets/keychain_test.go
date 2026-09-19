package secrets

import (
	"testing"
	"time"
)

func TestKeychainDescribeNamesTheBackend(t *testing.T) {
	if d := (Keychain{}).Describe(); d == "" {
		t.Fatal("Describe returned an empty string")
	}
}

// This is the regression test for a real incident: run inside this repo's
// own `go test ./... -race`, the pre-timeout Available() hung for the full
// ten-minute go test timeout in a session where the underlying `security`
// call didn't fail fast — it can pop a real permission dialog and wait
// forever for a human who isn't there. A caller (nanobotd's own startup)
// must never hang on this, so Available bounds it — see its doc comment.
// This asserts the bound holds with real margin, not that it holds exactly.
func TestAvailableNeverBlocksLongerThanItsTimeout(t *testing.T) {
	start := time.Now()
	Available()
	if elapsed := time.Since(start); elapsed > availabilityProbeTimeout+time.Second {
		t.Fatalf("Available took %s, want at most ~%s", elapsed, availabilityProbeTimeout)
	}
}

// TestKeychainRoundTripsWhenAvailable is the real proof this backend
// works — but only when the OS actually grants it. Verified live in this
// repo's own sandboxed dev shell that Available() is false here: the
// underlying `security` CLI refuses with "User interaction is not
// allowed" in a non-interactive session. Skipping instead of failing is
// the honest response to that, matching how internal/oneclaw's live tests
// skip rather than fail when no real API key is configured — this is a
// property of the environment, not of the code under test.
func TestKeychainRoundTripsWhenAvailable(t *testing.T) {
	if !Available() {
		t.Skip("OS keychain is not available in this session (needs an interactive session — see Keychain's doc comment)")
	}

	k := Keychain{}
	const key = "nanobots-secrets-test-roundtrip"
	t.Cleanup(func() { _ = k.Delete(key) })

	if err := k.Put(key, "test-value"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, found, err := k.Get(key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !found || got != "test-value" {
		t.Fatalf("Get: found=%v got=%q", found, got)
	}
	if err := k.Delete(key); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, found, _ := k.Get(key); found {
		t.Fatal("Get: still found after Delete")
	}
}
