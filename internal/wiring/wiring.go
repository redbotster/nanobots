// Package wiring builds the shared runtime objects both entry points need:
// nanobotd (internal/daemon) and the `nanobots run` CLI (cmd/nanobots).
//
// It exists because those two used to construct the same forty lines
// independently — the 1Claw vault, the per-provider step configs, the
// state/blob/run directories, the Orchestrator — and had already drifted
// apart in three ways that all mattered:
//
//   - the CLI built a non-persistent RunStore, so a run started with
//     `nanobots run` vanished the moment the command exited and never
//     appeared in history (see internal/runner/persist.go);
//   - only the daemon logged which providers were configured, so the CLI
//     gave no clue why a bot had quietly fallen back to demo data.
//
// Both are the kind of bug duplicated wiring produces — nobody decided
// them, they just drifted — so the wiring is here once instead. The two
// callers still differ where they genuinely should: the daemon takes an
// --env-file flag and serves on a fixed port; the CLI uses the default env
// file and an ephemeral one.
package wiring

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/redbotster/nanobots/internal/google"
	"github.com/redbotster/nanobots/internal/linkedin"
	"github.com/redbotster/nanobots/internal/oneclaw"
	"github.com/redbotster/nanobots/internal/runner"
	"github.com/redbotster/nanobots/internal/step"
	"github.com/redbotster/nanobots/internal/x"
)

// Paths are the directories nanobots owns under the user's home.
type Paths struct {
	StateDir       string // 1Claw agent credentials
	BlobDir        string // content-addressed file outputs
	RunWorkDir     string // per-run container workspaces
	FoundryWorkDir string // foundry job worktrees
	HistoryDir     string // persisted run history
}

// ResolvePaths locates (and creates where needed) everything under
// ~/.nanobots. Both entry points want the identical layout.
func ResolvePaths() (Paths, error) {
	stateDir, err := oneclaw.DefaultStateDir()
	if err != nil {
		return Paths{}, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, err
	}
	base := filepath.Join(home, ".nanobots")
	p := Paths{
		StateDir:       stateDir,
		BlobDir:        filepath.Join(base, "blobs"),
		RunWorkDir:     filepath.Join(base, "runs"),
		FoundryWorkDir: filepath.Join(base, "foundry"),
		HistoryDir:     filepath.Join(base, "history"),
	}
	if err := os.MkdirAll(p.RunWorkDir, 0o755); err != nil {
		return Paths{}, err
	}
	return p, nil
}

// ServiceConfigs are the per-provider step configs a live run needs. The
// zero value is entirely valid: it means "every service runs on demo
// fixtures", which is exactly what an unconfigured machine should do.
type ServiceConfigs struct {
	// VaultID is the shared "nanobots-main" vault every provider's
	// credential lives in. Exposed so the API can probe whether it's
	// currently locked; empty when 1Claw isn't configured.
	VaultID  string
	Google   step.GoogleConfig
	GitHub   step.GitHubConfig
	Slack    step.SlackConfig
	Stripe   step.StripeConfig
	HubSpot  step.HubSpotConfig
	X        step.XConfig
	LinkedIn step.LinkedInConfig
}

// Logf receives one line per configured provider. Pass nil to stay quiet.
type Logf func(format string, args ...any)

// BuildServiceConfigs resolves the shared 1Claw vault once (EnsureVault is a
// real network call, so not per-run) and fills in whichever providers have
// credentials available.
//
// GitHub/Slack/Stripe/HubSpot need only the vault id — their credential is a
// static token pasted in via Settings. Google/X/LinkedIn additionally need
// an OAuth client id from the env file, because those are refresh-token
// flows rather than static tokens. See docs/connections.md.
//
// An unconfigured provider is not an error: it stays zero and its bots run
// on fixtures.
func BuildServiceConfigs(oc *oneclaw.Client, envFilePath string, logf Logf) (ServiceConfigs, error) {
	var cfg ServiceConfigs
	if oc == nil || !oc.Configured() {
		return cfg, nil
	}
	if logf == nil {
		logf = func(string, ...any) {}
	}

	vault, err := oc.EnsureVault("nanobots-main")
	if err != nil {
		return cfg, fmt.Errorf("ensure 1Claw vault for connected-service credentials: %w", err)
	}
	cfg.VaultID = vault.ID
	cfg.GitHub = step.GitHubConfig{VaultID: vault.ID}
	cfg.Slack = step.SlackConfig{VaultID: vault.ID}
	cfg.Stripe = step.StripeConfig{VaultID: vault.ID}
	cfg.HubSpot = step.HubSpotConfig{VaultID: vault.ID}

	clientID, err := google.LoadClientID(envFilePath)
	if err != nil {
		return cfg, fmt.Errorf("load Google OAuth client id: %w", err)
	}
	if clientID != "" {
		cfg.Google = step.GoogleConfig{ClientID: clientID, VaultID: vault.ID}
		logf("google: OAuth client configured — run `nanobots connect google` once to link an account")
	}

	xClientID, err := x.LoadClientID(envFilePath)
	if err != nil {
		return cfg, fmt.Errorf("load X OAuth client id: %w", err)
	}
	if xClientID != "" {
		cfg.X = step.XConfig{ClientID: xClientID, VaultID: vault.ID}
		logf("x: OAuth client configured — connect an account from Settings")
	}

	liClientID, err := linkedin.LoadClientID(envFilePath)
	if err != nil {
		return cfg, fmt.Errorf("load LinkedIn OAuth client id: %w", err)
	}
	liClientSecret, err := linkedin.LoadClientSecret(envFilePath)
	if err != nil {
		return cfg, fmt.Errorf("load LinkedIn OAuth client secret: %w", err)
	}
	if liClientID != "" {
		cfg.LinkedIn = step.LinkedInConfig{ClientID: liClientID, ClientSecret: liClientSecret, VaultID: vault.ID}
		logf("linkedin: OAuth client configured — connect an account from Settings")
	}
	return cfg, nil
}

// OrchestratorOpts is what differs between the two entry points: the daemon
// serves on a fixed port, the CLI on an ephemeral one.
type OrchestratorOpts struct {
	RepoRoot     string
	BotsDir      string
	CallbackPort string // the port a bot container calls back on
}

// BuildOrchestrator assembles the runner from already-resolved pieces.
func BuildOrchestrator(
	opts OrchestratorOpts,
	paths Paths,
	oc *oneclaw.Client,
	svc ServiceConfigs,
	callbacks *runner.CallbackRegistry,
) *runner.Orchestrator {
	return &runner.Orchestrator{
		RepoRoot: opts.RepoRoot,
		BotsDir:  opts.BotsDir,
		// host.docker.internal, not localhost: this address is resolved
		// from inside a bot's container, not from this process.
		CallbackAddr:  "http://host.docker.internal:" + opts.CallbackPort,
		Callbacks:     callbacks,
		OneClaw:       oc,
		AgentStateDir: paths.StateDir,
		RunWorkDir:    paths.RunWorkDir,
		BlobDir:       paths.BlobDir,
		Google:        svc.Google,
		GitHub:        svc.GitHub,
		Slack:         svc.Slack,
		Stripe:        svc.Stripe,
		HubSpot:       svc.HubSpot,
		X:             svc.X,
		LinkedIn:      svc.LinkedIn,
	}
}

// BuildRunStore returns a store backed by the shared history directory, so a
// run started from the CLI and a run started from the WebUI land in the same
// place and both survive a restart. A history directory that can't be read
// is worth a warning, never a refusal to start.
func BuildRunStore(paths Paths, logf Logf) *runner.RunStore {
	store, err := runner.NewPersistentRunStore(paths.HistoryDir)
	if err != nil && logf != nil {
		logf("some run history could not be read from %s: %v", paths.HistoryDir, err)
	}
	return store
}
