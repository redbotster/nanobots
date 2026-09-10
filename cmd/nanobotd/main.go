// Command nanobotd is the Nanobots controller: REST+SSE API, planner,
// scheduler, context bus, and 1Claw bridge. See NANOBOTS-BLUEPRINT.md §3.1.
// This build implements the local target's REST+SSE API and Docker
// execution; the scheduler (cron triggers) and the kubernetes/apple compile
// targets are not implemented yet.
package main

import (
	"flag"
	"log"
	"path/filepath"

	"github.com/redbotster/nanobots/internal/daemon"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:7474", "address to listen on")
	repoRoot := flag.String("repo", ".", "path to the nanobots repo (for harness Dockerfiles)")
	botsDir := flag.String("bots", "", "path to the bots directory (default: <repo>/bots)")
	flag.Parse()

	root, err := filepath.Abs(*repoRoot)
	if err != nil {
		log.Fatal(err)
	}
	bots := *botsDir
	if bots == "" {
		bots = filepath.Join(root, "bots")
	}

	log.Fatal(daemon.Run(daemon.Options{Addr: *addr, RepoRoot: root, BotsDir: bots}))
}
