package runner

import (
	"log"
	"os"
	"path/filepath"

	"github.com/redbotster/nanobots/internal/schema"
)

// WarmHarnessImages builds or verifies every harness image the current bot
// catalog actually uses, in the background, so the first real run of the
// day doesn't pay a Docker build inline.
//
// Measured before building this: once an image is warm, `docker run`
// starts a container in well under a second regardless of image size —
// five runs each against harness-bare (25MB) and harness-openclaw (1.1GB,
// Chromium included) both landed under 1s. The actual cost this closes is
// the one-time build — apt-get installing Chromium alone runs tens of
// seconds — which EnsureHarnessImage otherwise pays lazily and inline, the
// first time some bot's run needs it. That turns a person's first press of
// Run into an unexplained wait with no run in the log yet to show why.
//
// Nothing here is required for correctness. A run still calls
// EnsureHarnessImage itself and gets a fresh build if warming hasn't
// finished, or hasn't started (no Docker, or nothing in the catalog needs
// a container yet) — this only decides whether that cost is paid before
// or during someone's first click.
func (o *Orchestrator) WarmHarnessImages() {
	if ok, _ := DockerAvailable(); !ok {
		return
	}
	types := harnessTypesInUse(o.BotsDir)
	if len(types) == 0 {
		return
	}
	go func() {
		for _, t := range types {
			if _, _, err := EnsureHarnessImage(t, o.RepoRoot, o.Version); err != nil {
				// Not fatal and not retried: the same build runs again,
				// synchronously, the first time an actual bot needs this
				// harness, and that failure is the one a user sees.
				log.Printf("warm harness image %q: %v", t, err)
				continue
			}
			log.Printf("harness image %q ready", t)
		}
	}()
}

// harnessTypesInUse scans botsDir for the distinct container images the
// current catalog actually needs — no point warming an image nothing runs
// in. Mirrors imageFor's declared-type-vs-needsBrowser promotion exactly
// (minus imageFor's run logging, which needs a live run to write to), so a
// bare-declared bot that renders is counted as openclaw here too. A bot
// that never touches Docker at all (runsInProcess) needs nothing warmed,
// and a bad nanobot.yaml is skipped rather than failing the scan, the same
// tolerance listBotSummaries already gives a broken bot directory.
func harnessTypesInUse(botsDir string) []string {
	entries, err := os.ReadDir(botsDir)
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var types []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		nb, err := schema.LoadNanobot(filepath.Join(botsDir, e.Name(), "nanobot.yaml"))
		if err != nil {
			continue
		}
		if inProcess, _ := runsInProcess(nb, ""); inProcess {
			continue
		}
		t := "bare"
		if needsBrowser(nb) {
			t = "openclaw"
		}
		if seen[t] {
			continue
		}
		seen[t] = true
		types = append(types, t)
	}
	return types
}
