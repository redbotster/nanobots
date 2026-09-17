// `nanobots run` — run a swarm to completion in this terminal,
// prompting on approvals.
package main

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/redbotster/nanobots/internal/api"
	"github.com/redbotster/nanobots/internal/oneclaw"
	"github.com/redbotster/nanobots/internal/remedy"
	"github.com/redbotster/nanobots/internal/roles"
	"github.com/redbotster/nanobots/internal/runner"
	"github.com/redbotster/nanobots/internal/step"
	"github.com/redbotster/nanobots/internal/wiring"
)

// runRun runs one swarm to completion without the WebUI: it starts the same
// orchestrator + callback server nanobotd would, on an ephemeral port, prints
// the log as it happens, and prompts on the terminal for any `approve` step
// — useful for testing a swarm end to end, and for scripting.
func runRun(args []string) error {
	var swarmPath, botsDir, repoRoot string
	botsDir = "bots"
	repoRoot = "."
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-f", "--file":
			i++
			swarmPath = args[i]
		case "--bots":
			i++
			botsDir = args[i]
		case "--repo":
			i++
			repoRoot = args[i]
		default:
			return fmt.Errorf("unknown flag %q", args[i])
		}
	}
	if swarmPath == "" {
		return fmt.Errorf("-f <swarm.yaml> is required")
	}

	root, err := filepath.Abs(repoRoot)
	if err != nil {
		return err
	}
	apiKey, err := oneclaw.LoadAPIKey("")
	if err != nil {
		return err
	}
	oc := oneclaw.NewClient(apiKey)
	if oc.Configured() {
		fmt.Println("1Claw: configured — bots run live where their services allow it")
	} else {
		fmt.Println("1Claw: no key configured — running fully in demo mode")
	}

	paths, err := wiring.ResolvePaths()
	if err != nil {
		return err
	}
	blobs, err := step.NewFSBlobStore(paths.BlobDir)
	if err != nil {
		return err
	}
	// "" resolves to $NANOBOTS_ENV_FILE then ~/.secrets/nanobots.env, the
	// same default LoadAPIKey used just above — this command has no
	// --env-file flag of its own, unlike nanobotd.
	svc, err := wiring.BuildServiceConfigs(oc, "", func(f string, a ...any) {
		fmt.Printf(f+"\n", a...)
	})
	if err != nil {
		return err
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	defer listener.Close()
	_, port, _ := net.SplitHostPort(listener.Addr().String())

	mem, err := wiring.BuildMemory(paths, "", oc, func(f string, a ...any) { fmt.Printf(f+"\n", a...) })
	if err != nil {
		return err
	}

	gen, err := wiring.BuildLLM("", oc, func(f string, a ...any) { fmt.Printf(f+"\n", a...) })
	if err != nil {
		return err
	}

	callbacks := runner.NewCallbackRegistry()
	orch := wiring.BuildOrchestrator(wiring.OrchestratorOpts{
		RepoRoot:     root,
		BotsDir:      filepath.Join(root, botsDir),
		CallbackPort: port,
	}, paths, oc, svc, callbacks)
	orch.Memory = mem
	orch.LLM = gen
	orch.Roles = &roles.Store{
		CatalogPath:  filepath.Join(root, "roles", "roles.yaml"),
		OverridePath: filepath.Join(paths.StateDir, "roles.json"),
	}
	// The same persistent store the daemon uses, so a run started here shows
	// up in the WebUI's Runs page and survives this command exiting.
	runs := wiring.BuildRunStore(paths, func(f string, a ...any) { fmt.Printf(f+"\n", a...) })
	srv := &api.Server{
		Orchestrator: orch,
		Runs:         runs,
		Callbacks:    callbacks,
		OneClaw:      oc,
		BotsDir:      filepath.Join(root, botsDir),
		Blobs:        blobs,
	}
	go http.Serve(listener, srv.Handler())

	run, err := orch.ExecuteSwarm(resolveSwarmPath(root, swarmPath))
	if err != nil {
		return err
	}
	// Add, not just construct the store: this command drives the
	// orchestrator directly rather than going through POST /api/runs, which
	// is the call that registers a run everywhere else. Without this the
	// store was built, handed to the server, and never told about the one
	// run this process actually makes.
	runs.Add(run)
	fmt.Printf("run %s: %s\n", run.ID, run.SwarmName)

	logCh := run.Subscribe()
	defer run.Unsubscribe(logCh)
	stdin := bufio.NewReader(os.Stdin)
	// Once stdin is exhausted nothing can ever say yes, so stop asking and
	// start declining — but say that's what happened. Detected from a read
	// rather than from isatty, so a piped `echo y | nanobots run` still
	// works.
	nobodyToAsk := false
	for {
		select {
		case entry, ok := <-logCh:
			if !ok {
				continue
			}
			fmt.Printf("[%s/%s] %s\n", entry.Bot, entry.Step, entry.Msg)
			for _, pa := range run.PendingApprovals() {
				decideApproval(run, pa, stdin, &nobodyToAsk)
			}
		case <-time.After(200 * time.Millisecond):
			// Through the getters: SetStatus writes these under a mutex
			// from the run's own goroutine, and reading the fields directly here
			// was a real data race the race detector never saw because no
			// test drives this loop.
			if status := run.GetStatus(); status == runner.StatusSucceeded || status == runner.StatusFailed {
				// Outcome, not status: a run you stopped and a run whose
				// approval you declined both end `failed`, and printing
				// that word about your own deliberate answer is the same
				// honesty bug the WebUI fixed for stopped runs a while ago.
				fmt.Printf("\nrun %s: %s\n", run.ID, run.Outcome())
				// A run that finished with a hole in it must not print as
				// an unqualified success — the whole point of continuing
				// past a failure is that someone still finds out.
				for _, t := range run.GetTolerated() {
					fmt.Printf("  continued past a failure in %s: %s\n", t.Bot, t.Error)
				}
				if msg := run.GetError(); msg != "" {
					fmt.Println("error:", msg)
					// The WebUI has shown the fix for these since they
					// existed; the CLI printed the bare error and left you
					// to work it out. A locked vault, a missing model, an
					// unconnected account all have a known one-sentence
					// answer, and "a failure says what to do about it" is
					// supposed to be true of this whole build, not just the
					// half of it with a browser.
					//
					// The action is dropped on purpose — it names a WebUI
					// page, and "click Settings" means nothing here.
					if r := remedy.For(msg); r != nil {
						fmt.Println()
						fmt.Println(" ", r.Advice)
						if r.Docs != "" {
							fmt.Println("  see", r.Docs)
						}
					}
					os.Exit(1)
				}
				return nil
			}
		}
	}
}

// resolveSwarmPath interprets -f relative to the repo root, and leaves an
// absolute path alone.
//
// It used to be an unconditional filepath.Join, which turns
// `-f /tmp/probe.yaml` into `<repo>/tmp/probe.yaml` — a file that doesn't
// exist, reported as a missing file at a path the user never typed. Worse,
// `nanobots plan -f` takes the same flag and doesn't do this, so the two
// commands disagreed about what one path meant.
func resolveSwarmPath(root, swarmPath string) string {
	if filepath.IsAbs(swarmPath) {
		return swarmPath
	}
	return filepath.Join(root, swarmPath)
}

// noTerminalDecider is what shows up as `decided_by` when the run declined
// an approval because there was no one to ask. It reads as an explanation
// in the failure message a declined gate produces.
const noTerminalDecider = "nobody — no terminal attached to ask"

// decideApproval asks the terminal, or declines when there isn't one.
//
// Failing closed is the point of an approval: an unattended run must never
// send, pay or delete because nobody was listening. But the old code got
// there by accident — ReadString returned EOF, the empty line didn't start
// with "y", and the run recorded `decided_by=cli` as though a person had
// sat there and typed no. The decision is the same; who made it is not.
func decideApproval(run *runner.Run, pa *runner.PendingApproval, stdin *bufio.Reader, nobodyToAsk *bool) {
	tier := pa.RiskTier
	if tier == "" {
		tier = "unspecified risk"
	}
	if *nobodyToAsk {
		fmt.Printf("\napproval needed (%s): %s\ndeclined — %s\n", tier, pa.Summary, noTerminalDecider)
		run.Decide(pa.ID, false, noTerminalDecider)
		return
	}
	fmt.Printf("\napproval needed (%s): %s\napprove? [y/N] ", tier, pa.Summary)
	line, err := stdin.ReadString('\n')
	if err != nil && strings.TrimSpace(line) == "" {
		*nobodyToAsk = true
		fmt.Printf("\ndeclined — %s\n", noTerminalDecider)
		run.Decide(pa.ID, false, noTerminalDecider)
		return
	}
	approved := strings.HasPrefix(strings.ToLower(strings.TrimSpace(line)), "y")
	run.Decide(pa.ID, approved, "cli")
}
