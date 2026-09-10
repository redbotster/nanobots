package oneclaw

import (
	"os"
	"testing"
)

// TestLiveReadOnlySmoke hits the real 1Claw API with a real key, but only
// ever calls read-only list endpoints — it never creates or deletes
// anything in the account. Skipped unless NANOBOTS_LIVE_TEST=1, so `go test
// ./...` stays offline and deterministic by default; this is the "prove it
// against production" check for anyone with a real key who wants to run it.
func TestLiveReadOnlySmoke(t *testing.T) {
	if os.Getenv("NANOBOTS_LIVE_TEST") == "" {
		t.Skip("set NANOBOTS_LIVE_TEST=1 to run this against the real 1Claw API")
	}
	key, err := LoadAPIKey("")
	if err != nil {
		t.Fatalf("LoadAPIKey: %v", err)
	}
	if key == "" {
		t.Skip("no ONECLAW_API_KEY configured")
	}
	c := NewClient(key)
	vaults, err := c.ListVaults()
	if err != nil {
		t.Fatalf("ListVaults: %v", err)
	}
	t.Logf("found %d vault(s)", len(vaults))
	agents, err := c.ListAgents()
	if err != nil {
		t.Fatalf("ListAgents: %v", err)
	}
	t.Logf("found %d agent(s)", len(agents))
}
