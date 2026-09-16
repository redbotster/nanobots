// Package daemon wires up everything nanobotd needs and serves it over
// HTTP. Shared by cmd/nanobotd (the standalone daemon binary) and cmd/nanobots
// up (which runs the same thing in-process for a one-command dev loop).
package daemon

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"path/filepath"

	"github.com/redbotster/nanobots/internal/api"
	"github.com/redbotster/nanobots/internal/foundry"
	"github.com/redbotster/nanobots/internal/oneclaw"
	"github.com/redbotster/nanobots/internal/roles"
	"github.com/redbotster/nanobots/internal/runner"
	"github.com/redbotster/nanobots/internal/scheduler"
	"github.com/redbotster/nanobots/internal/step"
	"github.com/redbotster/nanobots/internal/webui"
	"github.com/redbotster/nanobots/internal/wiring"
)

type Options struct {
	Addr        string // e.g. "127.0.0.1:7474"
	RepoRoot    string
	BotsDir     string
	EnvFilePath string // ONECLAW_API_KEY source; "" uses the default (~/.secrets/nanobots.env)
}

// Run builds the Orchestrator + API server and blocks serving HTTP.
// nanobotd binds loopback only — 127.0.0.1, never 0.0.0.0 — since the
// /internal/steps/* callback surface has no auth beyond a run's own token
// and was never meant to be reachable from outside this machine.
func Run(opts Options) error {
	if opts.Addr == "" {
		opts.Addr = "127.0.0.1:7474"
	}

	apiKey, err := oneclaw.LoadAPIKey(opts.EnvFilePath)
	if err != nil {
		return fmt.Errorf("load 1Claw API key: %w", err)
	}
	oc := oneclaw.NewClient(apiKey)
	if oc.Configured() {
		log.Println("1Claw: configured — bots run live where their services allow it")
	} else {
		log.Println("1Claw: no key configured — running fully in demo mode")
	}

	svc, err := wiring.BuildServiceConfigs(oc, opts.EnvFilePath, func(f string, a ...any) { log.Printf(f, a...) })
	if err != nil {
		return err
	}

	// Independent of 1Claw entirely — the foundry's sandboxed coding agent
	// needs its own Anthropic credential (see internal/foundry/agent_claude.go's
	// doc comment on why Shroud can't back this).
	anthropicKey, err := oneclaw.LoadEnvValue(opts.EnvFilePath, "ANTHROPIC_API_KEY")
	if err != nil {
		return fmt.Errorf("load Anthropic API key: %w", err)
	}
	if anthropicKey != "" {
		log.Println("foundry: ANTHROPIC_API_KEY configured — the composer can escalate a real gap to a coding agent")
	}

	paths, err := wiring.ResolvePaths()
	if err != nil {
		return err
	}
	blobs, err := step.NewFSBlobStore(paths.BlobDir)
	if err != nil {
		return err
	}

	mem, err := wiring.BuildMemory(paths, opts.EnvFilePath, oc, func(f string, a ...any) { log.Printf(f, a...) })
	if err != nil {
		return err
	}

	gen, err := wiring.BuildLLM(opts.EnvFilePath, oc, func(f string, a ...any) { log.Printf(f, a...) })
	if err != nil {
		return err
	}

	// The Shroud shim lets a client that can only be handed a base URL and
	// a bearer token — a local Honcho, say — still have its LLM spend
	// metered and its prompts redacted. Only worth exposing when there is a
	// 1Claw key to bill against.
	var shroudProxy *api.ShroudProxy
	if oc != nil && oc.Configured() {
		tok, err := api.LoadShroudProxyToken(paths.StateDir)
		if err != nil {
			return err
		}
		shroudProxy = &api.ShroudProxy{Token: tok, Provider: "anthropic"}
		log.Printf("shroud proxy: http://127.0.0.1:%s/shroud/v1 (token in %s)",
			portOf(opts.Addr), filepath.Join(paths.StateDir, "shroud-proxy-token"))
	}

	roleStore := &roles.Store{
		CatalogPath:  filepath.Join(opts.RepoRoot, "roles", "roles.yaml"),
		OverridePath: filepath.Join(paths.StateDir, "roles.json"),
	}

	webhookToken, err := api.LoadWebhookToken(paths.StateDir)
	if err != nil {
		return err
	}
	webhookTrigger := &api.WebhookTrigger{Token: webhookToken}
	log.Printf("webhooks: POST http://%s/webhooks/{swarm} (token in %s)",
		opts.Addr, filepath.Join(paths.StateDir, "webhook-token"))

	callbacks := runner.NewCallbackRegistry()
	orch := wiring.BuildOrchestrator(wiring.OrchestratorOpts{
		RepoRoot:     opts.RepoRoot,
		BotsDir:      opts.BotsDir,
		CallbackPort: portOf(opts.Addr),
	}, paths, oc, svc, callbacks)
	orch.Memory = mem
	orch.LLM = gen
	orch.Roles = roleStore

	foundryOrch := &foundry.Orchestrator{Config: foundry.Config{
		RepoRoot:      opts.RepoRoot,
		BotsDir:       opts.BotsDir,
		WorkDir:       paths.FoundryWorkDir,
		AgentStateDir: paths.StateDir,
		OneClaw:       oc,
		Agent:         &foundry.ClaudeCLIAgent{RepoRoot: opts.RepoRoot, APIKey: anthropicKey},
	}}

	runs := wiring.BuildRunStore(paths, func(f string, a ...any) { log.Printf(f, a...) })

	srv := &api.Server{
		Orchestrator: orch,
		Runs:         runs,
		Callbacks:    callbacks,
		OneClaw:      oc,
		BotsDir:      opts.BotsDir,
		Blobs:        blobs,
		Foundry:      foundryOrch,
		FoundryJobs:  foundry.NewJobStore(),
		EnvFilePath:  opts.EnvFilePath,
		VaultID:      svc.VaultID,
		Team:         &api.TeamStore{Path: filepath.Join(paths.StateDir, "team.json")},
		Shroud:       shroudProxy,
		Webhook:      webhookTrigger,
		Roles:        roleStore,
		UI:           webui.Handler(),
	}

	// Closes a real gap this build has had since its first commit: cron
	// triggers were declared in every catalog swarm's YAML but nothing
	// ever fired one — see internal/scheduler's own doc comment. Runs for
	// the life of the process, same as the HTTP server itself; no
	// graceful-shutdown story either has one yet.
	// The breaker is shared with the API so the app can show why a
	// schedule stopped and offer to start it again — a pause nobody can
	// see is just a schedule that mysteriously doesn't run.
	breaker := &scheduler.Breaker{Dir: paths.StateDir}
	srv.ScheduleBreaker = breaker
	sched := &scheduler.Scheduler{
		Orchestrator: orch,
		Runs:         srv.Runs,
		SwarmsDir:    filepath.Join(filepath.Dir(opts.BotsDir), "examples", "swarms"),
		Breaker:      breaker,
	}
	go sched.Run(context.Background())

	if webui.Available() {
		log.Printf("nanobots listening on http://%s — open that in a browser (bots: %s)", opts.Addr, opts.BotsDir)
	} else {
		// Said plainly rather than left for someone to discover as a blank
		// page: a binary built without `make ui` is the normal development
		// case, not a broken install.
		log.Printf("nanobotd listening on http://%s (API only, no UI in this binary; run `cd web && npm run dev`) (bots: %s)",
			opts.Addr, opts.BotsDir)
	}
	return http.ListenAndServe(opts.Addr, srv.Handler())
}

func portOf(addr string) string {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "7474"
	}
	return port
}
