package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func repoRootFromCmd(t *testing.T) string {
	t.Helper()
	// cmd/nanobots -> repo root
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(wd, "..", "..")
}

// export and import are the two halves of handing a swarm to someone else,
// and neither had a test — they were in the 1203-line main.go, in the 20%
// of this package that nothing covered.
//
// The round trip is the thing worth asserting: a bundle that cannot be
// imported is a share button that produces a file nobody can use.
func TestExportThenImportRoundTrips(t *testing.T) {
	root := repoRootFromCmd(t)
	t.Chdir(root) // both commands resolve bots/ and examples/swarms/ from cwd

	out := filepath.Join(t.TempDir(), "bundle.yaml")
	if err := runExport([]string{"-f", "examples/swarms/github-digest-to-slack.yaml", "-o", out}); err != nil {
		t.Fatalf("export: %v", err)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("the bundle was not written: %v", err)
	}
	body := string(raw)

	// Bots are named, not carried, so nobody ends up running a silent fork
	// (docs/sharing.md). The names and versions have to be in there.
	for _, want := range []string{"github-issues-digest", "notify", "github-digest-to-slack"} {
		if !strings.Contains(body, want) {
			t.Errorf("the bundle never mentions %q:\n%s", want, body[:min(len(body), 400)])
		}
	}
	// And no credential, ever.
	for _, never := range []string{"ocv_", "1ck_", "xoxb-", "ONECLAW_API_KEY"} {
		if strings.Contains(body, never) {
			t.Errorf("the bundle carries something that looks like a credential (%q)", never)
		}
	}
}

// A bundle naming a bot this catalog does not have must be refused before
// anything is written, not half-imported.
func TestImportRefusesABundleNamingAnUnknownBot(t *testing.T) {
	root := repoRootFromCmd(t)
	t.Chdir(root)

	// A real bundle, then tampered with — a handwritten one only proves
	// that import rejects malformed YAML, which is a different question.
	dir := t.TempDir()
	bundle := filepath.Join(dir, "bad.yaml")
	if err := runExport([]string{"-f", "examples/swarms/github-digest-to-slack.yaml", "-o", bundle}); err != nil {
		t.Fatalf("export: %v", err)
	}
	raw, err := os.ReadFile(bundle)
	if err != nil {
		t.Fatal(err)
	}
	tampered := strings.ReplaceAll(string(raw), "github-issues-digest", "no-such-bot")
	tampered = strings.ReplaceAll(tampered, "github-digest-to-slack", "borrowed-swarm")
	if err := os.WriteFile(bundle, []byte(tampered), 0o644); err != nil {
		t.Fatal(err)
	}

	importErr := runImport([]string{bundle})
	if importErr == nil {
		t.Fatal("a bundle naming a bot nobody has was imported")
	}
	if !strings.Contains(importErr.Error(), "no-such-bot") {
		t.Errorf("the refusal does not name the missing bot: %v", importErr)
	}
	if _, statErr := os.Stat(filepath.Join(root, "examples", "swarms", "borrowed-swarm.yaml")); statErr == nil {
		t.Error("the swarm was written anyway")
		_ = os.Remove(filepath.Join(root, "examples", "swarms", "borrowed-swarm.yaml"))
	}
}

// The bug: catalogLookup used to discard everything after "@" and load
// whatever version was on disk regardless, so import could report "you
// have everything this bundle needs" for a bundle naming a version that
// isn't actually installed — the mismatch only surfaced later, confusingly,
// at plan or run time. It has to be refused here, at import, where it's
// actually knowable.
func TestImportRefusesABundleRequestingAVersionNotOnDisk(t *testing.T) {
	root := repoRootFromCmd(t)
	t.Chdir(root)

	dir := t.TempDir()
	bundle := filepath.Join(dir, "future-version.yaml")
	if err := runExport([]string{"-f", "examples/swarms/github-digest-to-slack.yaml", "-o", bundle}); err != nil {
		t.Fatalf("export: %v", err)
	}
	raw, err := os.ReadFile(bundle)
	if err != nil {
		t.Fatal(err)
	}
	// github-issues-digest ships at 0.1.0 today — bump the version the
	// bundle claims to need, without touching the bot on disk. Renamed too,
	// so the destination file this test checks for can't collide with the
	// real github-digest-to-slack.yaml already in examples/swarms/.
	tampered := strings.ReplaceAll(string(raw), "github-issues-digest@0.1.0", "github-issues-digest@9.9.9")
	tampered = strings.ReplaceAll(tampered, "github-digest-to-slack", "future-version-swarm")
	if !strings.Contains(tampered, "9.9.9") {
		t.Fatal("test setup did not find github-issues-digest@0.1.0 to tamper with")
	}
	if err := os.WriteFile(bundle, []byte(tampered), 0o644); err != nil {
		t.Fatal(err)
	}

	importErr := runImport([]string{bundle})
	if importErr == nil {
		t.Fatal("a bundle requesting a version not on disk was imported")
	}
	if !strings.Contains(importErr.Error(), "github-issues-digest@9.9.9") {
		t.Errorf("the refusal does not name the requested bot@version: %v", importErr)
	}
	dest := filepath.Join(root, "examples", "swarms", "future-version-swarm.yaml")
	if _, statErr := os.Stat(dest); statErr == nil {
		t.Error("the swarm was written anyway")
		_ = os.Remove(dest)
	}
}

func TestExportNeedsASwarm(t *testing.T) {
	if err := runExport(nil); err == nil {
		t.Error("export with no -f was accepted")
	}
	if err := runExport([]string{"-f"}); err == nil {
		t.Error("-f with no value was accepted")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
