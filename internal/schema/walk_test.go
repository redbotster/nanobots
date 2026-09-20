package schema

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestForEachBotDirSkipsWhatIsNotAValidBot(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "real-bot", "nanobot.yaml"), "metadata:\n  name: real-bot\n")
	writeFile(t, filepath.Join(dir, "broken-bot", "nanobot.yaml"), "not: yaml: at: all: {\n")
	writeFile(t, filepath.Join(dir, "not-a-bot-dir", "readme.txt"), "just a file")
	writeFile(t, filepath.Join(dir, "stray-file.txt"), "not a directory at all")

	var ids []string
	if err := ForEachBotDir(dir, func(id string, nb *Nanobot) {
		ids = append(ids, id)
		if nb.Metadata.Name != "real-bot" {
			t.Errorf("got bot named %q, want real-bot", nb.Metadata.Name)
		}
	}); err != nil {
		t.Fatalf("ForEachBotDir: %v", err)
	}

	if want := []string{"real-bot"}; len(ids) != 1 || ids[0] != want[0] {
		t.Errorf("ids = %v, want %v — a broken bot, a non-bot directory, or a stray file was not skipped", ids, want)
	}
}

func TestForEachBotDirReturnsTheReadDirError(t *testing.T) {
	if err := ForEachBotDir(filepath.Join(t.TempDir(), "does-not-exist"), func(string, *Nanobot) {}); err == nil {
		t.Error("expected an error for a missing bots directory")
	}
}

func TestForEachSwarmFileSkipsWhatIsNotAValidSwarm(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "real-swarm.yaml"), "metadata:\n  name: real-swarm\n")
	writeFile(t, filepath.Join(dir, "broken.yaml"), "not: yaml: at: all: {\n")
	writeFile(t, filepath.Join(dir, "notes.txt"), "not a swarm file")
	writeFile(t, filepath.Join(dir, "a-directory.yaml", "nested.yaml"), "metadata:\n  name: nested\n")

	var names []string
	if err := ForEachSwarmFile(dir, func(path string, sw *Nanoswarm) bool {
		names = append(names, sw.Metadata.Name)
		return true
	}); err != nil {
		t.Fatalf("ForEachSwarmFile: %v", err)
	}

	if want := []string{"real-swarm"}; len(names) != 1 || names[0] != want[0] {
		t.Errorf("names = %v, want %v — a broken swarm, a non-yaml file, or a directory named *.yaml was not skipped", names, want)
	}
}

func TestForEachSwarmFileStopsWhenFnReturnsFalse(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a-swarm.yaml"), "metadata:\n  name: a-swarm\n")
	writeFile(t, filepath.Join(dir, "b-swarm.yaml"), "metadata:\n  name: b-swarm\n")

	var seen int
	err := ForEachSwarmFile(dir, func(path string, sw *Nanoswarm) bool {
		seen++
		return false // stop after the first match, like a name lookup would
	})
	if err != nil {
		t.Fatalf("ForEachSwarmFile: %v", err)
	}
	if seen != 1 {
		t.Errorf("fn was called %d times, want exactly 1 — returning false should stop the scan", seen)
	}
}
