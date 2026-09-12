// Command nanobots is the Nanobots CLI: init, add, plan, up, run, save,
// publish, compile. See NANOBOTS-BLUEPRINT.md §6. This build implements
// `plan` and `conform`; the rest are stubbed with a clear "not yet" message
// rather than silently doing nothing.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/redbotster/nanobots/internal/api"
	"github.com/redbotster/nanobots/internal/contract"
	"github.com/redbotster/nanobots/internal/daemon"
	"github.com/redbotster/nanobots/internal/google"
	"github.com/redbotster/nanobots/internal/oneclaw"
	"github.com/redbotster/nanobots/internal/planner"
	"github.com/redbotster/nanobots/internal/runner"
	"github.com/redbotster/nanobots/internal/schema"
	"github.com/redbotster/nanobots/internal/step"
	"github.com/redbotster/nanobots/internal/wiring"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cmd, args := os.Args[1], os.Args[2:]
	var err error
	switch cmd {
	case "plan":
		err = runPlan(args)
	case "conform":
		err = runConform(args)
	case "schema":
		err = runSchema(args)
	case "up":
		err = runUp(args)
	case "run":
		err = runRun(args)
	case "connect":
		err = runConnect(args)
	case "init", "add", "save", "publish", "compile":
		fmt.Fprintf(os.Stderr, "nanobots %s: not implemented in this build yet\n", cmd)
		os.Exit(1)
	case "-h", "--help", "help":
		usage()
		return
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", cmd)
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage: nanobots <command> [flags]

commands:
  plan -f <swarm.yaml> [--bots <dir>]     type-check a swarm's snaps and print its run DAG
  conform <bot-dir> [--fixtures <dir>]    run a bot's conformance fixtures against its declared ports
  schema --out <dir>                      regenerate schemas/*.json from the Go types in internal/schema
  up [--addr host:port]                   start nanobotd (REST+SSE API) in the foreground
  run -f <swarm.yaml> [--bots <dir>]       run a swarm to completion, printing its log; prompts on approvals
  connect google                          link a real Gmail/Drive/Sheets account (one-time OAuth in your browser)
  init, add, save, publish, compile        not implemented in this build yet`)
}

func runPlan(args []string) error {
	var swarmPath, botsDir string
	botsDir = "bots"
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-f", "--file":
			i++
			if i >= len(args) {
				return fmt.Errorf("%s requires a value", args[i-1])
			}
			swarmPath = args[i]
		case "--bots":
			i++
			if i >= len(args) {
				return fmt.Errorf("--bots requires a value")
			}
			botsDir = args[i]
		default:
			return fmt.Errorf("unknown flag %q", args[i])
		}
	}
	if swarmPath == "" {
		return fmt.Errorf("-f <swarm.yaml> is required")
	}
	result, err := planner.Plan(swarmPath, botsDir)
	if err != nil {
		return err
	}
	fmt.Print(result.Report())
	if !result.OK() {
		os.Exit(1)
	}
	return nil
}

func runConform(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: nanobots conform <bot-dir> [--fixtures <dir>]")
	}
	botDir := args[0]
	fixturesDir := ""
	for i := 1; i < len(args); i++ {
		if args[i] == "--fixtures" && i+1 < len(args) {
			i++
			fixturesDir = args[i]
		}
	}
	report, err := contract.RunConformance(botDir, fixturesDir)
	if err != nil {
		return err
	}
	fmt.Print(report.String())
	if !report.OK() {
		os.Exit(1)
	}
	return nil
}

func runSchema(args []string) error {
	out := "schemas"
	for i := 0; i < len(args); i++ {
		if args[i] == "--out" && i+1 < len(args) {
			i++
			out = args[i]
		}
	}
	if err := writeSchema(out+"/nanobot.schema.json", schema.NanobotJSONSchema()); err != nil {
		return err
	}
	if err := writeSchema(out+"/nanoswarm.schema.json", schema.NanoswarmJSONSchema()); err != nil {
		return err
	}
	fmt.Printf("wrote %s/nanobot.schema.json and %s/nanoswarm.schema.json\n", out, out)
	return nil
}

func writeSchema(path string, doc map[string]any) error {
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

func runUp(args []string) error {
	addr := "127.0.0.1:7474"
	for i := 0; i < len(args); i++ {
		if args[i] == "--addr" && i+1 < len(args) {
			i++
			addr = args[i]
		}
	}
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	return daemon.Run(daemon.Options{Addr: addr, RepoRoot: root, BotsDir: filepath.Join(root, "bots")})
}

// runConnect handles `nanobots connect <service>`. Today that's just
// "google": a one-time interactive OAuth round trip (see
// internal/google.Connect) whose refresh token gets stored in 1Claw's vault,
// never on local disk — every bot with a Google service and
// `connection: oauth_native` then shares this one connected account (see
// docs/connections.md).
func runConnect(args []string) error {
	if len(args) != 1 || args[0] != "google" {
		return fmt.Errorf("usage: nanobots connect google")
	}
	clientID, err := google.LoadClientID("")
	if err != nil {
		return err
	}
	if clientID == "" {
		return fmt.Errorf("GOOGLE_OAUTH_CLIENT_ID is not set — add it to ~/.secrets/nanobots.env " +
			"(a Google Cloud \"Desktop app\" OAuth client id; see docs/connections.md)")
	}
	apiKey, err := oneclaw.LoadAPIKey("")
	if err != nil {
		return err
	}
	oc := oneclaw.NewClient(apiKey)
	if !oc.Configured() {
		return fmt.Errorf("ONECLAW_API_KEY is not set — the connected account's refresh token needs a 1Claw vault to live in")
	}

	fmt.Println("Opening your browser to sign in to Google — grant access, then come back here.")
	tr, err := google.Connect(context.Background(), clientID, google.DefaultScopes)
	if err != nil {
		return err
	}
	if tr.RefreshToken == "" {
		return fmt.Errorf("google did not return a refresh token — try again (this can happen if consent wasn't re-prompted)")
	}

	vault, err := oc.EnsureVault("nanobots-main")
	if err != nil {
		return fmt.Errorf("ensure 1Claw vault: %w", err)
	}
	if err := oc.PutSecret(vault.ID, "google/refresh_token", tr.RefreshToken); err != nil {
		return fmt.Errorf("store refresh token in 1Claw vault: %w", err)
	}
	fmt.Println("Connected. Gmail/Drive/Sheets bots with connection: oauth_native can now run live " +
		"(restart nanobotd if it's already running).")
	return nil
}

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
				fmt.Printf("\nrun %s: %s\n", run.ID, status)
				if msg := run.GetError(); msg != "" {
					fmt.Println("error:", msg)
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
