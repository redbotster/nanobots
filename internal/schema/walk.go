package schema

import (
	"os"
	"path/filepath"
	"strings"
)

// ForEachBotDir lists botsDir's subdirectories and calls fn once for every
// one that parses as a nanobot.yaml. A directory that isn't a bot — a
// stray README, a half-written one, anything that fails to parse — is
// silently skipped rather than failing the whole scan, matching what every
// caller of this scan already did on its own before this was pulled out:
// "not every dir under bots/ need be a bot". id is the directory name.
func ForEachBotDir(botsDir string, fn func(id string, nb *Nanobot)) error {
	entries, err := os.ReadDir(botsDir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		nb, err := LoadNanobot(filepath.Join(botsDir, e.Name(), "nanobot.yaml"))
		if err != nil {
			continue
		}
		fn(e.Name(), nb)
	}
	return nil
}

// ForEachSwarmFile lists swarmsDir's *.yaml files, in directory order, and
// calls fn once for every one that parses as a nanoswarm.yaml — a
// subdirectory, a non-.yaml file, or one that fails to parse is skipped,
// not an error, the same tolerance every caller of this scan already had
// on its own. fn returns false to stop the scan early, for a caller that
// only wants the first match.
func ForEachSwarmFile(swarmsDir string, fn func(path string, sw *Nanoswarm) bool) error {
	entries, err := os.ReadDir(swarmsDir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		path := filepath.Join(swarmsDir, e.Name())
		sw, err := LoadNanoswarm(path)
		if err != nil {
			continue
		}
		if !fn(path, sw) {
			return nil
		}
	}
	return nil
}
