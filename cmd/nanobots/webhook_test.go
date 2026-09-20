package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindSwarmByNameMatchesByMetadataNameOrFilename(t *testing.T) {
	dir := t.TempDir()
	// Filename deliberately differs from metadata.name, so a lookup by
	// either one can be told apart from the other.
	yaml := "apiVersion: nanobots.dev/v1alpha1\nkind: Nanoswarm\nmetadata:\n  name: real-name\nspec:\n  trigger: { type: manual }\n"
	if err := os.WriteFile(filepath.Join(dir, "on-disk-filename.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, sw, err := findSwarmByName(dir, "real-name"); err != nil || sw.Metadata.Name != "real-name" {
		t.Errorf("lookup by metadata name failed: sw=%+v err=%v", sw, err)
	}
	if _, sw, err := findSwarmByName(dir, "on-disk-filename"); err != nil || sw.Metadata.Name != "real-name" {
		t.Errorf("lookup by filename failed: sw=%+v err=%v", sw, err)
	}
	if _, _, err := findSwarmByName(dir, "no-such-swarm"); err == nil {
		t.Error("expected an error for a name that matches nothing")
	}
}
