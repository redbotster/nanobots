// Package daemon wires up everything nanobotd needs and serves it over
// HTTP. Shared by cmd/nanobotd (the standalone daemon binary) and cmd/nanobots
// up (which runs the same thing in-process for a one-command dev loop).
package daemon

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"

	"github.com/redbotster/nanobots/internal/api"
	"github.com/redbotster/nanobots/internal/google"
	"github.com/redbotster/nanobots/internal/oneclaw"
	"github.com/redbotster/nanobots/internal/runner"
	"github.com/redbotster/nanobots/internal/step"
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
	if oc.Configured() {
		vault, err := oc.EnsureVault("nanobots-main")
		if err != nil {
			return fmt.Errorf("ensure 1Claw vault for connected-service credentials: %w", err)
		}
		githubCfg = step.GitHubConfig{VaultID: vault.ID}
		slackCfg = step.SlackConfig{VaultID: vault.ID}

		clientID, err := google.LoadClientID(opts.EnvFilePath)
		if err != nil {
			return fmt.Errorf("load Google OAuth client id: %w", err)
		}
		if clientID != "" {
			googleCfg = step.GoogleConfig{ClientID: clientID, VaultID: vault.ID}
			log.Println("google: OAuth client configured — run `nanobots connect google` once to link an account")
		}
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
	}

	srv := &api.Server{
		Orchestrator: orch,
		Runs:         runner.NewRunStore(),
		Callbacks:    callbacks,
		OneClaw:      oc,
		BotsDir:      opts.BotsDir,
		Blobs:        blobs,
	}

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
