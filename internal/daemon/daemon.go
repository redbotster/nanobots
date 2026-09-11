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
	"os"
	"path/filepath"

	"github.com/redbotster/nanobots/internal/api"
	"github.com/redbotster/nanobots/internal/foundry"
	"github.com/redbotster/nanobots/internal/google"
	"github.com/redbotster/nanobots/internal/linkedin"
	"github.com/redbotster/nanobots/internal/oneclaw"
	"github.com/redbotster/nanobots/internal/runner"
	"github.com/redbotster/nanobots/internal/scheduler"
	"github.com/redbotster/nanobots/internal/step"
	"github.com/redbotster/nanobots/internal/x"
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

	// Google/GitHub/Slack all keep their credentials in the same 1Claw vault
	// ("nanobots-main") — resolved once here, not per-run, since EnsureVault
	// is a real network call. GitHub/Slack just need that vault id (a token
	// pasted in via Settings or `nanobots connect`); Google additionally
	// needs an OAuth client id, since it's a refresh-token flow, not a
	// static token — see internal/step/{google,github,slack}_live.go and
	// docs/connections.md.
	var googleCfg step.GoogleConfig
	var githubCfg step.GitHubConfig
	var slackCfg step.SlackConfig
	var stripeCfg step.StripeConfig
	var hubspotCfg step.HubSpotConfig
	var xCfg step.XConfig
	var linkedinCfg step.LinkedInConfig
	if oc.Configured() {
		vault, err := oc.EnsureVault("nanobots-main")
		if err != nil {
			return fmt.Errorf("ensure 1Claw vault for connected-service credentials: %w", err)
		}
		githubCfg = step.GitHubConfig{VaultID: vault.ID}
		slackCfg = step.SlackConfig{VaultID: vault.ID}
		stripeCfg = step.StripeConfig{VaultID: vault.ID}
		hubspotCfg = step.HubSpotConfig{VaultID: vault.ID}

		clientID, err := google.LoadClientID(opts.EnvFilePath)
		if err != nil {
			return fmt.Errorf("load Google OAuth client id: %w", err)
		}
		if clientID != "" {
			googleCfg = step.GoogleConfig{ClientID: clientID, VaultID: vault.ID}
			log.Println("google: OAuth client configured — run `nanobots connect google` once to link an account")
		}

		xClientID, err := x.LoadClientID(opts.EnvFilePath)
		if err != nil {
			return fmt.Errorf("load X OAuth client id: %w", err)
		}
		if xClientID != "" {
			xCfg = step.XConfig{ClientID: xClientID, VaultID: vault.ID}
			log.Println("x: OAuth client configured — connect an account from Settings")
		}

		liClientID, err := linkedin.LoadClientID(opts.EnvFilePath)
		if err != nil {
			return fmt.Errorf("load LinkedIn OAuth client id: %w", err)
		}
		liClientSecret, err := linkedin.LoadClientSecret(opts.EnvFilePath)
		if err != nil {
			return fmt.Errorf("load LinkedIn OAuth client secret: %w", err)
		}
		if liClientID != "" {
			linkedinCfg = step.LinkedInConfig{ClientID: liClientID, ClientSecret: liClientSecret, VaultID: vault.ID}
			log.Println("linkedin: OAuth client configured — connect an account from Settings")
		}
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

	stateDir, err := oneclaw.DefaultStateDir()
	if err != nil {
		return err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	blobDir := filepath.Join(home, ".nanobots", "blobs")
	blobs, err := step.NewFSBlobStore(blobDir)
	if err != nil {
		return err
	}
	runWorkDir := filepath.Join(home, ".nanobots", "runs")
	if err := os.MkdirAll(runWorkDir, 0o755); err != nil {
		return err
	}
	foundryWorkDir := filepath.Join(home, ".nanobots", "foundry")

	callbacks := runner.NewCallbackRegistry()
	orch := &runner.Orchestrator{
		RepoRoot:      opts.RepoRoot,
		BotsDir:       opts.BotsDir,
		CallbackAddr:  "http://host.docker.internal:" + portOf(opts.Addr),
		Callbacks:     callbacks,
		OneClaw:       oc,
		AgentStateDir: stateDir,
		RunWorkDir:    runWorkDir,
		BlobDir:       blobDir,
		Google:        googleCfg,
		GitHub:        githubCfg,
		Slack:         slackCfg,
		Stripe:        stripeCfg,
		HubSpot:       hubspotCfg,
		X:             xCfg,
		LinkedIn:      linkedinCfg,
	}

	foundryOrch := &foundry.Orchestrator{Config: foundry.Config{
		RepoRoot:      opts.RepoRoot,
		BotsDir:       opts.BotsDir,
		WorkDir:       foundryWorkDir,
		AgentStateDir: stateDir,
		OneClaw:       oc,
		Agent:         &foundry.ClaudeCLIAgent{RepoRoot: opts.RepoRoot, APIKey: anthropicKey},
	}}

	// Run history survives a restart (see internal/runner/persist.go). A
	// history directory that can't be read is worth a warning, not a
	// refusal to start — NewPersistentRunStore returns a usable store
	// either way.
	runs := runner.NewRunStore()
	if historyDir, err := runner.DefaultHistoryDir(); err != nil {
		fmt.Fprintf(os.Stderr, "nanobotd: run history disabled: %v\n", err)
	} else if store, err := runner.NewPersistentRunStore(historyDir); err != nil {
		fmt.Fprintf(os.Stderr, "nanobotd: some run history could not be read from %s: %v\n", historyDir, err)
		runs = store
	} else {
		runs = store
	}

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
	}

	// Closes a real gap this build has had since its first commit: cron
	// triggers were declared in every catalog swarm's YAML but nothing
	// ever fired one — see internal/scheduler's own doc comment. Runs for
	// the life of the process, same as the HTTP server itself; no
	// graceful-shutdown story either has one yet.
	sched := &scheduler.Scheduler{
		Orchestrator: orch,
		Runs:         srv.Runs,
		SwarmsDir:    filepath.Join(filepath.Dir(opts.BotsDir), "examples", "swarms"),
	}
	go sched.Run(context.Background())

	log.Printf("nanobotd listening on http://%s (bots: %s)", opts.Addr, opts.BotsDir)
	return http.ListenAndServe(opts.Addr, srv.Handler())
}

func portOf(addr string) string {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "7474"
	}
	return port
}
