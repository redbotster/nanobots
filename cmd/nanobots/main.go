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
	"github.com/redbotster/nanobots/internal/roles"
	"github.com/redbotster/nanobots/internal/runner"
	"github.com/redbotster/nanobots/internal/schema"
	"github.com/redbotster/nanobots/internal/service"
	"github.com/redbotster/nanobots/internal/share"
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
	case "service":
		err = runService(args)
	case "connectors":
		err = runConnectors(args)
	case "spend":
		err = runSpend(args)
	case "export":
		err = runExport(args)
	case "import":
		err = runImport(args)
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
  service install|status|uninstall        keep nanobotd running across reboots, so cron triggers actually fire
  connectors list|register|install|status one place to register an OAuth app and wire it to a bot
  spend                                   what this account has spent on models
  export -f <swarm.yaml> [-o <file>]      bundle a swarm to hand to someone else
  import <bundle.yaml>                    add a shared swarm to examples/swarms/
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
				fmt.Printf("\nrun %s: %s\n", run.ID, status)
				// A run that finished with a hole in it must not print as
				// an unqualified success — the whole point of continuing
				// past a failure is that someone still finds out.
				for _, t := range run.GetTolerated() {
					fmt.Printf("  continued past a failure in %s: %s\n", t.Bot, t.Error)
				}
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

// runService manages the background job that keeps nanobotd alive.
//
// Fourteen of the fifteen catalog swarms carry a cron trigger and the
// scheduler that fires them works — but only while nanobotd is running,
// which meant a terminal window someone remembered to leave open. A
// catalog written entirely in the future tense has to survive a reboot.
//
// Every path is resolved and printed rather than assumed: this writes a
// file into someone's LaunchAgents, and the least it can do is say exactly
// what it wrote, how to load it, and how to undo it.
func runService(args []string) error {
	action := "status"
	if len(args) > 0 {
		action = args[0]
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	switch action {
	case "status":
		path, ok := service.Installed(home)
		if !ok {
			fmt.Printf("not installed (would be %s)\n", path)
			fmt.Println("run `nanobots service install` to keep nanobotd running across reboots")
			return nil
		}
		load, unload := service.LaunchctlHint(path)
		fmt.Printf("installed: %s\n", path)
		fmt.Printf("  load:   %s\n  unload: %s\n", load, unload)
		return nil

	case "install":
		if !service.Supported() {
			return fmt.Errorf("service install is macOS-only in this build")
		}
		bin, err := os.Executable()
		if err != nil {
			return err
		}
		if bin, err = filepath.EvalSymlinks(bin); err != nil {
			return err
		}
		root, err := os.Getwd()
		if err != nil {
			return err
		}
		// A job pointing at `go run`'s temporary binary would work until
		// the next reboot and then not, in a way nobody would connect back
		// to this command.
		if strings.Contains(bin, os.TempDir()) || strings.Contains(bin, "go-build") {
			return fmt.Errorf("this looks like a `go run` build at %s, which will not exist after a reboot —\n"+
				"build it first (go build -o bin/nanobots ./cmd/nanobots) and run that", bin)
		}

		addr := "127.0.0.1:7474"
		for i := 1; i < len(args); i++ {
			if args[i] == "--addr" && i+1 < len(args) {
				i++
				addr = args[i]
			}
		}
		logDir := filepath.Join(home, ".nanobots", "logs")
		path, err := service.Write(home, service.Config{
			Binary: bin, RepoRoot: root, Addr: addr, LogDir: logDir,
		})
		if err != nil {
			return err
		}
		load, unload := service.LaunchctlHint(path)
		fmt.Printf("wrote %s\n", path)
		fmt.Printf("  runs:    %s up --addr %s\n", bin, addr)
		fmt.Printf("  in:      %s\n", root)
		fmt.Printf("  logs:    %s/nanobotd.log\n", logDir)
		fmt.Println()
		fmt.Printf("start it now:  %s\n", load)
		fmt.Printf("undo:          %s && nanobots service uninstall\n", unload)
		fmt.Println()
		fmt.Println(service.DescribeMissedRuns)
		return nil

	case "uninstall":
		path, ok := service.Installed(home)
		if !ok {
			fmt.Println("not installed, nothing to remove")
			return nil
		}
		_, unload := service.LaunchctlHint(path)
		if _, err := service.Remove(home); err != nil {
			return err
		}
		fmt.Printf("removed %s\n", path)
		fmt.Printf("if it is still running: %s\n", unload)
		return nil
	}
	return fmt.Errorf("usage: nanobots service install|status|uninstall")
}

// oneClawClient builds a configured client or explains what's missing.
func oneClawClient() (*oneclaw.Client, error) {
	apiKey, err := oneclaw.LoadAPIKey("")
	if err != nil {
		return nil, err
	}
	oc := oneclaw.NewClient(apiKey)
	if !oc.Configured() {
		return nil, fmt.Errorf("ONECLAW_API_KEY is not set in ~/.secrets/nanobots.env")
	}
	return oc, nil
}

// runConnectors uses 1Claw's own reviewed OAuth applications instead of
// making you register your own.
//
// This is the wall the project has had all along: pointing a bot at a real
// account meant a Google Cloud project, a consent screen and a client id,
// and thirteen of the catalog's bots are Google bots that stayed on demo
// fixtures until someone did that. 1Claw already holds apps for Gmail,
// Calendar, Sheets, Slack, GitHub and X. One install, one browser round
// trip, done.
func runConnectors(args []string) error {
	oc, err := oneClawClient()
	if err != nil {
		return err
	}
	action := "list"
	if len(args) > 0 {
		action = args[0]
	}

	switch action {
	case "list":
		presets, err := oc.ListConnectorPresets()
		if err != nil {
			return err
		}
		fmt.Printf("%d connectors available through 1Claw — no provider console needed:\n\n", len(presets))
		for _, p := range presets {
			name := p.DisplayName
			if name == "" {
				name = p.Slug
			}
			fmt.Printf("  %-16s %s\n", p.Slug, name)
		}
		fmt.Println("\nnanobots connectors install <bot> <slug>   e.g. inbox-triage gmail")
		return nil

	case "install":
		if len(args) < 3 {
			return fmt.Errorf("usage: nanobots connectors install <bot-id> <connector-slug>\n" +
				"  the bot is which nanobot gets the credential (its agent), e.g. inbox-triage")
		}
		botID, slug := args[1], args[2]
		// The binding must be named after the service the bot declares, or
		// LiveDeps.ServiceCall will look for one that isn't there.
		bindingName := slug
		for i := 3; i < len(args); i++ {
			if args[i] == "--as" && i+1 < len(args) {
				i++
				bindingName = args[i]
			}
		}

		agentID, err := agentForBot(oc, botID)
		if err != nil {
			return err
		}

		got, err := oc.InstallConnector(agentID, slug, bindingName, nil)
		if err != nil {
			return err
		}
		fmt.Printf("installed %s on %s as binding %q\n", got.PresetSlug, botID, got.BindingName)
		if got.NextStep != "" {
			fmt.Printf("\n%s\n", got.NextStep)
		}
		if got.AuthorizationURL != "" {
			fmt.Printf("\nOpen this to grant access:\n  %s\n", got.AuthorizationURL)
		}
		fmt.Printf("\nThen set the bot's service to connection: oauth_1claw and it will use the real account.\n")
		return nil

	case "register":
		// The step every OAuth connector needs first. 1Claw does not ship
		// shared OAuth apps: an install answers "No app credentials
		// configured" until this has been done for that provider.
		if len(args) < 5 {
			return fmt.Errorf("usage: nanobots connectors register <bot-id> <provider> <client-id> <client-secret>\n" +
				"  providers: google slack github x notion discord hubspot linkedin microsoft salesforce\n" +
				"  the secret goes straight to 1Claw and is never written to disk here")
		}
		agentID, err := agentForBot(oc, args[1])
		if err != nil {
			return err
		}
		if err := oc.SaveOAuthAppCredentials(agentID, args[2], args[3], args[4], ""); err != nil {
			return err
		}
		fmt.Printf("registered a %s OAuth app for %s\n", args[2], args[1])
		fmt.Printf("now: nanobots connectors install %s <slug>\n", args[1])
		return nil

	case "status":
		if len(args) < 2 {
			return fmt.Errorf("usage: nanobots connectors status <bot-id>")
		}
		agentID, err := agentForBot(oc, args[1])
		if err != nil {
			return err
		}
		if creds, err := oc.ListOAuthAppCredentials(agentID); err == nil && len(creds) > 0 {
			for _, c := range creds {
				fmt.Printf("  oauth app        %s registered\n", c.Provider)
			}
		}
		list, err := oc.ListAgentConnectors(agentID)
		if err != nil {
			return err
		}
		if len(list) == 0 {
			fmt.Printf("%s has no connectors installed\n", args[1])
			return nil
		}
		for _, c := range list {
			state := "installed, not yet authorised"
			if c.Connected {
				state = "connected"
			}
			fmt.Printf("  %-16s %s\n", c.BindingName, state)
		}
		return nil
	}
	return fmt.Errorf("usage: nanobots connectors list|install|status")
}

// runSpend answers the question a scheduled product has to be able to
// answer: what did last night cost.
func runSpend(args []string) error {
	oc, err := oneClawClient()
	if err != nil {
		return err
	}
	b, err := oc.LLMTokenBillingStatus()
	if err != nil {
		return err
	}
	if !b.Enabled {
		fmt.Println("1Claw is not billing this account's model tokens, so it has no figure to report.")
		fmt.Println("Model spend on a direct provider key is between you and that provider.")
		return nil
	}
	if cents, known := b.Spent(); known {
		fmt.Printf("this billing period: $%.2f\n", float64(cents)/100)
		if b.CycleUsage.PeriodStart != "" {
			fmt.Printf("  since %s\n", b.CycleUsage.PeriodStart)
		}
	} else {
		fmt.Println("this billing period: nothing metered yet")
	}
	if cb := b.CreditBalance; cb != nil && (cb.AvailableCents > 0 || cb.UsedCents > 0) {
		fmt.Printf("credit: $%.2f available, $%.2f used\n",
			float64(cb.AvailableCents)/100, float64(cb.UsedCents)/100)
	}
	if b.Warning != "" {
		fmt.Printf("\n%s\n", b.Warning)
	}
	return nil
}

// agentForBot resolves (creating if needed) the 1Claw agent a bot's
// credentials hang off. A connector is installed on an agent, and this
// build gives each bot its own.
func agentForBot(oc *oneclaw.Client, botID string) (string, error) {
	root, err := os.Getwd()
	if err != nil {
		return "", err
	}
	nb, err := schema.LoadNanobot(filepath.Join(root, "bots", botID, "nanobot.yaml"))
	if err != nil {
		return "", fmt.Errorf("no bot %q in bots/: %w", botID, err)
	}
	stateDir, err := oneclaw.DefaultStateDir()
	if err != nil {
		return "", err
	}
	id, _, err := oc.EnsureAgent(stateDir, "nanobots-"+nb.Metadata.Name,
		oneclaw.CreateAgentRequest{ShroudEnabled: true, MemoryEnabled: true})
	if err != nil {
		return "", fmt.Errorf("ensure this bot's 1Claw agent: %w", err)
	}
	return id, nil
}

// catalogLookup resolves an id@version reference against bots/.
func catalogLookup(botsDir string) func(string) (*schema.Nanobot, error) {
	return func(use string) (*schema.Nanobot, error) {
		id, _, _ := strings.Cut(use, "@")
		return schema.LoadNanobot(filepath.Join(botsDir, id, "nanobot.yaml"))
	}
}

// runExport bundles a swarm so it can be handed to someone else.
//
// A swarm you build has been stuck on the machine that built it: the
// catalog was the only way to get one. The bundle carries the file itself,
// which bots it needs, which accounts the recipient will have to connect,
// and — the part worth surfacing — what it can write to when it runs.
func runExport(args []string) error {
	var swarmPath, out string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-f", "--file":
			i++
			if i < len(args) {
				swarmPath = args[i]
			}
		case "-o", "--out":
			i++
			if i < len(args) {
				out = args[i]
			}
		}
	}
	if swarmPath == "" {
		return fmt.Errorf("usage: nanobots export -f <swarm.yaml> [-o <file>]")
	}
	raw, err := os.ReadFile(swarmPath)
	if err != nil {
		return err
	}
	sw, err := schema.LoadNanoswarm(swarmPath)
	if err != nil {
		return err
	}
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	bundle, err := share.Export(raw, sw, catalogLookup(filepath.Join(root, "bots")))
	if err != nil {
		return err
	}
	body, err := bundle.Marshal()
	if err != nil {
		return err
	}
	if out == "" {
		// To stdout, so it pipes. A bundle is text on purpose.
		fmt.Print(string(body))
		return nil
	}
	if err := os.WriteFile(out, body, 0o644); err != nil {
		return err
	}
	fmt.Printf("wrote %s — %d bot(s)", out, len(bundle.Requires))
	if len(bundle.Acts) > 0 {
		fmt.Printf(", can write to %s", strings.Join(bundle.Acts, ", "))
	}
	fmt.Println()
	return nil
}

// runImport adds a shared swarm to the local catalog.
//
// Everything is checked before anything is written. A swarm referencing a
// bot you do not have is not importable, and discovering that at run time —
// after it has been saved and looks legitimate — is the worse order.
func runImport(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: nanobots import <bundle.yaml>")
	}
	raw, err := os.ReadFile(args[0])
	if err != nil {
		return err
	}
	bundle, err := share.Parse(raw)
	if err != nil {
		return err
	}
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	botsDir := filepath.Join(root, "bots")
	missing := bundle.Missing(func(use string) bool {
		nb, err := catalogLookup(botsDir)(use)
		return err == nil && nb != nil
	})
	if len(missing) > 0 {
		return fmt.Errorf("this bundle needs bots you do not have: %s\n"+
			"nothing was written — add them to bots/ and import again", strings.Join(missing, ", "))
	}

	dest := filepath.Join(root, "examples", "swarms", slugify(bundle.Name)+".yaml")
	if _, err := os.Stat(dest); err == nil {
		return fmt.Errorf("%s already exists — rename the swarm in the bundle, or move the existing file", dest)
	}
	if err := os.WriteFile(dest, []byte(bundle.Swarm), 0o644); err != nil {
		return err
	}

	fmt.Printf("imported %s\n", dest)
	if len(bundle.Connects) > 0 {
		fmt.Printf("  it wants these accounts connected: %s\n", strings.Join(bundle.Connects, ", "))
	}
	if len(bundle.Acts) > 0 {
		fmt.Printf("  when it runs it can write to: %s\n", strings.Join(bundle.Acts, ", "))
	}
	fmt.Printf("\ncheck it before running:  nanobots plan -f %s\n", dest)
	return nil
}

// slugify is main's own copy of the builder's rule, so an imported swarm
// lands with the same kind of filename a saved one does.
func slugify(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	var b strings.Builder
	lastDash := true
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		out = "imported-swarm"
	}
	return out
}
