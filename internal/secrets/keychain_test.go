package secrets

import "testing"

func TestKeychainDescribeNamesTheBackend(t *testing.T) {
	if d := (Keychain{}).Describe(); d == "" {
		t.Fatal("Describe returned an empty string")
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
