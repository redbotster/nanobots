package botpkg

import (
	"os"
	"path/filepath"
	"testing"
)

func writeBot(t *testing.T, dir, name, version string) {
	t.Helper()
	botDir := filepath.Join(dir, name)
	if err := os.MkdirAll(botDir, 0o755); err != nil {
		t.Fatal(err)
	}
	yaml := `apiVersion: nanobots.dev/v1alpha1
kind: Nanobot
metadata:
  name: ` + name + `
  version: ` + version + `
spec:
  harness: { type: bare }
  ports: { inputs: [], outputs: [] }
  steps: []
`
	if err := os.WriteFile(filepath.Join(botDir, "nanobot.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLocalDirResolvesABareName(t *testing.T) {
	dir := t.TempDir()
	writeBot(t, dir, "notify", "0.1.0")

	nb, err := (LocalDir{Dir: dir}).Resolve("notify", "")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if nb.Metadata.Name != "notify" {
		t.Errorf("Metadata.Name = %q, want notify", nb.Metadata.Name)
	}
}

func TestLocalDirResolvesAMatchingVersion(t *testing.T) {
	dir := t.TempDir()
	writeBot(t, dir, "notify", "0.2.0")

	if _, err := (LocalDir{Dir: dir}).Resolve("notify", "0.2.0"); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
}

// This is the exact bug fixed by routing every caller through one Source:
// a version mismatch has to be a real, refused error, not something a
// caller can silently ignore by discarding the "@version" suffix.
func TestLocalDirRefusesAMismatchedVersion(t *testing.T) {
	dir := t.TempDir()
	writeBot(t, dir, "notify", "0.1.0")

	_, err := (LocalDir{Dir: dir}).Resolve("notify", "9.9.9")
	if err == nil {
		t.Fatal("expected an error for a version that does not match what's on disk")
	}
	for _, want := range []string{"notify", "9.9.9", "0.1.0"} {
		if !contains(err.Error(), want) {
			t.Errorf("error does not mention %q: %v", want, err)
		}
	}
}

func TestLocalDirResolveMissingBotErrors(t *testing.T) {
	dir := t.TempDir()
	if _, err := (LocalDir{Dir: dir}).Resolve("nothing-here", ""); err == nil {
		t.Fatal("expected an error for a bot that does not exist")
	}
}

func TestLocalDirListsOnlyDirectoriesWithAManifest(t *testing.T) {
	dir := t.TempDir()
	writeBot(t, dir, "notify", "0.1.0")
	writeBot(t, dir, "github-issues-digest", "0.1.0")
	// A plain file and an empty directory should not show up as bots.
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "not-a-bot"), 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := (LocalDir{Dir: dir}).List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	want := []string{"github-issues-digest", "notify"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("List() = %v, want %v", got, want)
	}
}

func TestParseRefSplitsNameAndVersion(t *testing.T) {
	cases := []struct{ ref, name, version string }{
		{"notify@0.1.0", "notify", "0.1.0"},
		{"notify", "notify", ""},
		{"lead-enricher@0.2.0", "lead-enricher", "0.2.0"},
	}
	for _, c := range cases {
		name, version := ParseRef(c.ref)
		if name != c.name || version != c.version {
			t.Errorf("ParseRef(%q) = (%q, %q), want (%q, %q)", c.ref, name, version, c.name, c.version)
		}
	}
}

func contains(haystack, needle string) bool {
	return len(needle) == 0 || (len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0)
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
