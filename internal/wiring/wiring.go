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
	"github.com/redbotster/nanobots/internal/llm"
	"github.com/redbotster/nanobots/internal/memory"
	"github.com/redbotster/nanobots/internal/oneclaw"
	"github.com/redbotster/nanobots/internal/runner"
	"github.com/redbotster/nanobots/internal/step"
	"github.com/redbotster/nanobots/internal/x"
	"strings"
)

// Paths are the directories nanobots owns under the user's home.
type Paths struct {
	StateDir       string // 1Claw agent credentials
	BlobDir        string // content-addressed file outputs
	RunWorkDir     string // per-run container workspaces
	FoundryWorkDir string // foundry job worktrees
	HistoryDir     string // persisted run history
	MemoryDir      string // what bots remember between runs
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
		MemoryDir:      filepath.Join(base, "memory"),
	}
	if err := os.MkdirAll(p.RunWorkDir, 0o755); err != nil {
		return Paths{}, err
	}
	return p, nil
}

// ServiceConfigs are the per-provider step configs a live run needs. The
// zero value is entirely valid: it means "every service runs on demo
// fixtures", which is exactly what an unconfigured machine should do.
// An alias, not a copy: this used to be its own struct with the same seven
// fields, which meant every provider added here had to be added again in
// internal/step and unpacked field by field in between. One type, resolved
// here and carried unchanged all the way to the bot.
type ServiceConfigs = step.ServiceConfigs

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
		Services:      svc,
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

	// The history directory has always been bounded. The per-run workspaces
	// beside it never were, and nothing removed one — 266 directories and
	// 14MB against 200 retained runs on the machine this was found on, 66 of
	// them belonging to runs no longer in any list. Pruned here, against the
	// runs history actually kept, so the two cannot disagree about what
	// still exists.
	//
	// Startup is the safe moment: nothing is running, so no container holds
	// a bind mount into any of these.
	keep := map[string]bool{}
	for _, r := range store.List() {
		keep[r.ID] = true
	}
	removed, perr := runner.PruneWorkDirs(paths.RunWorkDir, keep)
	if logf != nil {
		if perr != nil {
			logf("cleaning old run workspaces in %s: %v", paths.RunWorkDir, perr)
		}
		if removed > 0 {
			logf("cleaned %d run workspace(s) older than the kept history", removed)
		}
	}

	// The same for the file contents those runs produced — the last
	// unbounded store under ~/.nanobots. Keyed by digest rather than by run
	// id, because blobs are content-addressed and two runs that produced
	// identical bytes share one file.
	digests := map[string]bool{}
	for _, r := range store.List() {
		for _, d := range runner.BlobRefs(r) {
			digests[d] = true
		}
	}
	blobsRemoved, freed, berr := runner.PruneBlobs(paths.BlobDir, digests)
	if logf != nil {
		if berr != nil {
			logf("cleaning unreferenced files in %s: %v", paths.BlobDir, berr)
		}
		if blobsRemoved > 0 {
			logf("cleaned %d file(s) no kept run refers to (%.1f MB)", blobsRemoved, float64(freed)/(1<<20))
		}
	}
	return store
}

// BuildMemory chooses the backend behind every memory.* step.
//
// Configured through the same dotenv file as everything else:
//
//	NANOBOTS_MEMORY      local (default) | 1claw | honcho
//	HONCHO_URL           e.g. http://localhost:8000 for a self-hosted server
//	HONCHO_WORKSPACE     defaults to "nanobots"
//	HONCHO_API_KEY       omit entirely for a self-hosted server, which runs
//	                     with AUTH_USE_AUTH=false by default
//
// local is the default deliberately. Memory used to require 1Claw, so the
// two bots that use it behaved differently depending on a credential
// unrelated to what they were remembering — competitor-watch reported
// everything as new on every run without a key. Local files make memory
// work out of the box.
//
// honcho composes rather than replaces: key/value stays on local disk,
// because Honcho has no key/value semantics and pretending otherwise would
// be a lie about what it does. What Honcho adds is recall — accumulate
// observations, ask questions in plain language.
func BuildMemory(paths Paths, envFilePath string, oc *oneclaw.Client, logf Logf) (memory.Store, error) {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	kind, err := oneclaw.LoadEnvValue(envFilePath, "NANOBOTS_MEMORY")
	if err != nil {
		return nil, fmt.Errorf("read NANOBOTS_MEMORY: %w", err)
	}
	local, err := memory.NewLocal(paths.MemoryDir)
	if err != nil {
		return nil, err
	}

	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "", "local":
		logf("memory: local files under %s", paths.MemoryDir)
		return local, nil

	case "1claw", "oneclaw":
		if oc == nil || !oc.Configured() {
			logf("memory: NANOBOTS_MEMORY=1claw but no 1Claw key is configured — falling back to local files")
			return local, nil
		}
		// The agent id is per-bot and only known at run time, so the 1Claw
		// backend is built per bot in internal/runner. Local is the
		// placeholder for anything that needs a store before then.
		logf("memory: 1Claw agent memory (key/value only; recall is unavailable)")
		return &memory.DeferredOneClaw{Fallback: local}, nil

	case "honcho":
		url, err := oneclaw.LoadEnvValue(envFilePath, "HONCHO_URL")
		if err != nil {
			return nil, err
		}
		if url == "" {
			return nil, fmt.Errorf("NANOBOTS_MEMORY=honcho needs HONCHO_URL (e.g. http://localhost:8000)")
		}
		workspace, _ := oneclaw.LoadEnvValue(envFilePath, "HONCHO_WORKSPACE")
		if workspace == "" {
			workspace = "nanobots"
		}
		apiKey, _ := oneclaw.LoadEnvValue(envFilePath, "HONCHO_API_KEY")
		logf("memory: local files for key/value, Honcho at %s (workspace %q) for recall", url, workspace)
		return &memory.Composite{KV: local, Rich: memory.NewHoncho(url, workspace, apiKey)}, nil
	}
	return nil, fmt.Errorf("NANOBOTS_MEMORY=%q is not a backend this build has (local, 1claw, honcho)", kind)
}

// BuildLLM chooses what an ai.generate step's prompt is sent to.
//
// Configured through the same dotenv file as everything else:
//
//	NANOBOTS_LLM         shroud (default when 1Claw is configured) |
//	                     anthropic | openai | gemini | none
//	ANTHROPIC_API_KEY    used by NANOBOTS_LLM=anthropic
//	OPENAI_API_KEY       used by NANOBOTS_LLM=openai
//	OPENAI_BASE_URL      point openai at any chat-completions endpoint —
//	                     OpenRouter, Together, Groq, vLLM, LiteLLM, Ollama
//	GEMINI_API_KEY       used by NANOBOTS_LLM=gemini
//	NANOBOTS_LLM_MODEL   the model to use when a bot asks for a provider
//	                     this backend doesn't serve (see llm.resolveModel)
//
// Shroud is the default whenever 1Claw is configured, and that is a
// deliberate preference rather than alphabetical luck: it is the only
// backend that bills tokens against a per-agent budget, redacts PII and
// secrets before a prompt leaves the machine, and screens for injection.
// Those are the protections this project uses 1Claw for. A direct provider
// key is a fallback for people who don't have 1Claw — not an equal option.
//
// Which is why a direct key alone is now enough to run live at all. It
// wasn't: ai.generate called Shroud directly, so without 1Claw every bot in
// the catalog silently fell back to demo fixtures no matter what other keys
// were configured.
func BuildLLM(envFilePath string, oc *oneclaw.Client, logf Logf) (llm.Generator, error) {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	kind, err := oneclaw.LoadEnvValue(envFilePath, "NANOBOTS_LLM")
	if err != nil {
		return nil, fmt.Errorf("read NANOBOTS_LLM: %w", err)
	}
	model, _ := oneclaw.LoadEnvValue(envFilePath, "NANOBOTS_LLM_MODEL")
	kind = strings.ToLower(strings.TrimSpace(kind))

	// Unset means "work out what this machine has", so that adding one key
	// to the env file is the whole setup step.
	if kind == "" {
		kind = autoDetectLLM(envFilePath, oc)
	}

	switch kind {
	case "none":
		logf("llm: none configured — ai.generate steps will use demo fixtures")
		return nil, nil

	case "shroud", "1claw", "oneclaw":
		if oc == nil || !oc.Configured() {
			return nil, fmt.Errorf("NANOBOTS_LLM=shroud needs ONECLAW_API_KEY")
		}
		// A Shroud client is per-agent and a bot's agent is provisioned
		// when the run reaches it, so internal/runner swaps in the real one
		// per bot. The fallback covers anything generating outside a bot
		// run.
		fallback, _ := directLLM(envFilePath, model, func(string, ...any) {})
		logf("llm: 1Claw Shroud — token billing, per-agent budgets, PII redaction and injection screening")
		return &llm.DeferredShroud{Fallback: fallback}, nil

	case "anthropic", "openai", "gemini":
		g, err := namedDirectLLM(envFilePath, kind, model)
		if err != nil {
			return nil, err
		}
		logf("llm: %s — no 1Claw in the path, so no budget ceiling, PII redaction or injection screening", g.Describe())
		return g, nil
	}
	return nil, fmt.Errorf("NANOBOTS_LLM=%q is not a backend this build has (shroud, anthropic, openai, gemini, none)", kind)
}

// autoDetectLLM picks a backend from what's actually configured, preferring
// Shroud for its guardrails and otherwise taking the first direct key
// present. Order among the direct three is fixed rather than clever, so the
// answer doesn't change between machines with the same env file.
func autoDetectLLM(envFilePath string, oc *oneclaw.Client) string {
	if oc != nil && oc.Configured() {
		return "shroud"
	}
	for _, c := range []struct{ env, kind string }{
		{"ANTHROPIC_API_KEY", "anthropic"},
		{"OPENAI_API_KEY", "openai"},
		{"GEMINI_API_KEY", "gemini"},
	} {
		if v, _ := oneclaw.LoadEnvValue(envFilePath, c.env); v != "" {
			return c.kind
		}
	}
	return "none"
}

// directLLM builds whichever direct provider is configured, or nil. Used as
// Shroud's stand-in before a bot's agent exists.
func directLLM(envFilePath, model string, logf Logf) (llm.Generator, error) {
	kind := autoDetectLLM(envFilePath, nil)
	if kind == "none" {
		return nil, nil
	}
	return namedDirectLLM(envFilePath, kind, model)
}

func namedDirectLLM(envFilePath, kind, model string) (llm.Generator, error) {
	switch kind {
	case "anthropic":
		key, _ := oneclaw.LoadEnvValue(envFilePath, "ANTHROPIC_API_KEY")
		if key == "" {
			return nil, fmt.Errorf("NANOBOTS_LLM=anthropic needs ANTHROPIC_API_KEY")
		}
		return llm.NewAnthropic(key, model), nil
	case "openai":
		key, _ := oneclaw.LoadEnvValue(envFilePath, "OPENAI_API_KEY")
		if key == "" {
			return nil, fmt.Errorf("NANOBOTS_LLM=openai needs OPENAI_API_KEY")
		}
		base, _ := oneclaw.LoadEnvValue(envFilePath, "OPENAI_BASE_URL")
		return llm.NewOpenAI(key, base, model), nil
	case "gemini":
		key, _ := oneclaw.LoadEnvValue(envFilePath, "GEMINI_API_KEY")
		if key == "" {
			return nil, fmt.Errorf("NANOBOTS_LLM=gemini needs GEMINI_API_KEY")
		}
		return llm.NewGemini(key, model), nil
	}
	return nil, fmt.Errorf("no direct provider named %q", kind)
}
