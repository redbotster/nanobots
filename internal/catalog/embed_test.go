package catalog

import (
	"os"
	"path/filepath"
	"testing"
)

// A binary built without `make catalog` must say so plainly, the same way
// internal/webui does for a UI-less binary — the error is what a released
// build's own startup path surfaces, not a mystery "open bots: no such
// file or directory" three layers down.
func TestWithoutACatalogFSSaysSo(t *testing.T) {
	if Available() {
		t.Skip("this binary has a catalog built in; the not-built path is what this covers")
	}
	if _, err := FS(); err == nil {
		t.Fatal("FS() succeeded with no catalog built in")
	}
	if err := ExtractEmbedded(t.TempDir()); err == nil {
		t.Fatal("ExtractEmbedded() succeeded with no catalog built in")
	}
}

// With a real catalog built in (run `make catalog` first), extracting it
// must produce the exact layout a repo checkout has, so a caller can point
// RepoRoot/BotsDir at the result unchanged.
func TestTheRealEmbeddedCatalogExtracts(t *testing.T) {
	if !Available() {
		t.Skip("no catalog in this binary; run `make catalog` to exercise the real embed")
	}
	dir := t.TempDir()
	if err := ExtractEmbedded(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "bots", "inbox-triage", "nanobot.yaml")); err != nil {
		t.Errorf("bots/inbox-triage/nanobot.yaml missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "roles", "roles.yaml")); err != nil {
		t.Errorf("roles/roles.yaml missing: %v", err)
	}
	matches, err := filepath.Glob(filepath.Join(dir, "examples", "swarms", "*.yaml"))
	if err != nil || len(matches) == 0 {
		t.Errorf("no swarms extracted into examples/swarms: %v, %v", matches, err)
	}
}
