// `nanobots up` — the daemon, the API and the WebUI on one port.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/redbotster/nanobots/internal/daemon"
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
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	return daemon.Run(daemon.Options{Addr: addr, RepoRoot: root, BotsDir: filepath.Join(root, "bots")})
}
