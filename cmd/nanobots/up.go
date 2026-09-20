// `nanobots up` — the daemon, the API and the WebUI on one port.
package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/redbotster/nanobots/internal/catalog"
	"github.com/redbotster/nanobots/internal/daemon"
	"github.com/redbotster/nanobots/internal/wiring"
)

func runUp(args []string) error {
	addr := "127.0.0.1:7474"
	// Anything unrecognised is refused rather than ignored. It used to be
	// ignored, and the way that surfaced is worth keeping: the container
	// image's entrypoint was `nanobots up --addr 0.0.0.0:7474`, so
	// `docker run <image> nanobots version` appended "nanobots version" to
	// it — and instead of printing a version, the daemon started, bound a
	// port, and fired three scheduled swarms. A flag parser that drops what
	// it does not understand turns a typo into a different program.
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--addr":
			i++
			if i >= len(args) {
				return fmt.Errorf("--addr requires a host:port")
			}
			addr = args[i]
		default:
			// -h/--help never reaches here: helpFor handles it before
			// dispatch, for every command at once.
			return fmt.Errorf("unknown argument %q\nusage: nanobots up [--addr host:port]", args[i])
		}
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	root, botsDir, err := resolveRoot(cwd, catalogRoot)
	if err != nil {
		return err
	}
	return daemon.Run(daemon.Options{Addr: addr, RepoRoot: root, BotsDir: botsDir, Version: Version})
}

// resolveRoot decides what to run against: cwd itself, if it has a bots/
// directory (a git checkout of this repo, the normal dev case), or
// whatever fallback returns otherwise — split out from runUp so the
// decision can be tested without starting a server that never returns.
func resolveRoot(cwd string, fallback func() (string, error)) (root, botsDir string, err error) {
	botsDir = filepath.Join(cwd, "bots")
	if _, statErr := os.Stat(botsDir); statErr == nil {
		return cwd, botsDir, nil
	}
	root, err = fallback()
	if err != nil {
		return "", "", fmt.Errorf("no bots/ in %s, and could not fall back to this binary's own catalog: %w", cwd, err)
	}
	return root, filepath.Join(root, "bots"), nil
}

// catalogRoot extracts this binary's embedded catalog (bots/,
// examples/swarms/, roles/roles.yaml) to ~/.nanobots/catalog if it isn't
// already there, and returns that directory to stand in as RepoRoot —
// laid out exactly like a checkout's root, so everything downstream that
// reads bots/, examples/swarms/ or roles/roles.yaml relative to RepoRoot
// needs no separate code path.
//
// Docker-backed bots still need harness/*/Dockerfile, which this does not
// carry (see docs/harnesses.md) — those bots fail with a clear "no such
// file" until the harness images ship over a registry instead.
func catalogRoot() (string, error) {
	paths, err := wiring.ResolvePaths()
	if err != nil {
		return "", err
	}
	if err := catalog.ExtractEmbedded(paths.CatalogDir); err != nil {
		return "", err
	}
	log.Printf("no git checkout found here — running this binary's own built-in catalog from %s", paths.CatalogDir)
	return paths.CatalogDir, nil
}
