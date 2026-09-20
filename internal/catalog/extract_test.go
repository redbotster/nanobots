package catalog

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

// The extraction logic (extract/hashFS/copyFS) is tested against a fake
// fs.FS rather than the real embed, so it gets real coverage in CI even
// though CI never runs `make catalog` first — see internal/webui's tests
// for the same gap, accepted there because there was no way around it for
// an actual embed.FS. This package's embed.go and embed_test.go still test
// the real thing, skipping when Available() is false.

func fakeCatalog() fstest.MapFS {
	return fstest.MapFS{
		"bots/inbox-triage/nanobot.yaml": {Data: []byte("name: inbox-triage\n")},
		"examples/swarms/morning-brief.yaml": {
			Data: []byte("metadata:\n  name: morning-brief\n"),
		},
		"roles/roles.yaml": {Data: []byte("roles: []\n")},
	}
}

func TestExtractLaysOutTheCatalogAtTheGivenDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := extract(fakeCatalog(), dir); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"bots/inbox-triage/nanobot.yaml",
		"examples/swarms/morning-brief.yaml",
		"roles/roles.yaml",
	} {
		if _, err := os.Stat(filepath.Join(dir, want)); err != nil {
			t.Errorf("%s missing after extraction: %v", want, err)
		}
	}
	got, err := os.ReadFile(filepath.Join(dir, "bots/inbox-triage/nanobot.yaml"))
	if err != nil || string(got) != "name: inbox-triage\n" {
		t.Errorf("bots/inbox-triage/nanobot.yaml = %q, %v", got, err)
	}
}

// The whole point of the marker: a second extraction of the same content
// does no work — this is what makes it cheap to call on every `nanobots
// up`, not just the first.
func TestExtractIsANoOpWhenTheContentIsUnchanged(t *testing.T) {
	dir := t.TempDir()
	src := fakeCatalog()
	if err := extract(src, dir); err != nil {
		t.Fatal(err)
	}
	// Prove it really did nothing the second time: touch a file that a
	// real extraction would have overwritten, and confirm it survives.
	canary := filepath.Join(dir, "bots/inbox-triage/nanobot.yaml")
	if err := os.WriteFile(canary, []byte("modified by the test"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := extract(src, dir); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(canary)
	if err != nil || string(got) != "modified by the test" {
		t.Errorf("a no-op extraction touched the file it shouldn't have: %q, %v", got, err)
	}
}

// A binary upgrade (a new catalog) has to actually replace what an older
// one extracted — including removing a file the new catalog no longer
// carries, not just overwriting the ones it still does.
func TestExtractReplacesStaleContentOnAChange(t *testing.T) {
	dir := t.TempDir()
	if err := extract(fakeCatalog(), dir); err != nil {
		t.Fatal(err)
	}

	changed := fstest.MapFS{
		"bots/inbox-triage/nanobot.yaml": {Data: []byte("name: inbox-triage\nversion: 2\n")},
		"roles/roles.yaml":               {Data: []byte("roles: []\n")},
		// morning-brief.yaml deliberately dropped.
	}
	if err := extract(changed, dir); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(filepath.Join(dir, "bots/inbox-triage/nanobot.yaml"))
	if err != nil || string(got) != "name: inbox-triage\nversion: 2\n" {
		t.Errorf("stale content survived a real change: %q, %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "examples/swarms/morning-brief.yaml")); err == nil {
		t.Error("a file dropped from the catalog is still on disk after re-extraction")
	}
}

func TestHashFSNoticesARename(t *testing.T) {
	a, err := hashFS(fstest.MapFS{"one": {Data: []byte("x")}, "two": {Data: []byte("y")}})
	if err != nil {
		t.Fatal(err)
	}
	b, err := hashFS(fstest.MapFS{"one": {Data: []byte("y")}, "two": {Data: []byte("x")}})
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Error("swapping two files' content hashed the same as the original — a rename with the same bytes would be missed")
	}
}
