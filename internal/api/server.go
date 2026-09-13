// Package api is nanobotd's REST+SSE surface: the WebUI's backend, and the
// callback target every bot container calls into (see
// internal/step.RemoteDeps and cmd/nanobot-agent). Go 1.22+'s http.ServeMux
// method+wildcard patterns are enough here — no router dependency needed,
// matching the blueprint's "keep the initial dependency list small" rule.
package api

import (
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/redbotster/nanobots/internal/foundry"
	"github.com/redbotster/nanobots/internal/llm"
	"github.com/redbotster/nanobots/internal/memory"
	"github.com/redbotster/nanobots/internal/oneclaw"
	"github.com/redbotster/nanobots/internal/roles"
	"github.com/redbotster/nanobots/internal/runner"
	"github.com/redbotster/nanobots/internal/schema"
	"github.com/redbotster/nanobots/internal/step"
)

// Server holds everything the HTTP handlers need.
type Server struct {
	Orchestrator *runner.Orchestrator
	Runs         *runner.RunStore
	Callbacks    *runner.CallbackRegistry
	OneClaw      *oneclaw.Client
	BotsDir      string
	Blobs        step.BlobStore
	// SwarmsDir is where handleListSwarms scans and handleSaveSwarm writes —
	// kept as its own explicit field (not derived from BotsDir) specifically
	// so tests can point it at a t.TempDir() instead of ever writing into
	// this repo's real examples/swarms/. Defaults to
	// filepath.Join(filepath.Dir(BotsDir), "examples", "swarms") when empty.
	SwarmsDir string

	// Foundry/FoundryJobs back the compose escalation path (see
	// internal/foundry and internal/api/foundry.go) — nil is fine (the same
	// "not configured" shape every other optional integration here uses);
	// handleStartFoundryJob returns a clear error rather than a nil-pointer
	// panic when they're unset.
	Foundry     *foundry.Orchestrator
	FoundryJobs *foundry.JobStore

	// EnvFilePath is where handleSetupOneClawKey writes ONECLAW_API_KEY —
	// "" resolves to oneclaw.DefaultEnvFilePath(), the same file every
	// other credential-loading call in this build already reads from.
	EnvFilePath string

	// VaultID is the shared 1Claw vault every connected service's credential
	// lives in ("nanobots-main"), resolved once at startup by
	// internal/wiring. Empty means 1Claw isn't configured, and the lock
	// probe stays silent.
	VaultID string

	// docker and vault cache their probes behind the status endpoint. Zero
	// values are ready to use.
	docker dockerProbe
	vault  vaultProbe

	// Team remembers which bots the user has tuned. nil disables the
	// Team view rather than failing anything.
	Team *TeamStore

	// Roles is the review-role library shown in Team. nil leaves the
	// endpoints reporting an empty library rather than failing.
	Roles *roles.Store

	// Shroud, when set, exposes /shroud/v1/* — an OpenAI-shaped endpoint
	// that adds the headers Shroud needs, so a client that can only be
	// given a base URL and a bearer token (Honcho) can still have its spend
	// metered and its prompts redacted. nil leaves the route returning 404.
	Shroud *ShroudProxy

	// Webhook, when set, exposes POST /webhooks/{swarm} — the third
	// trigger type, declared since the first commit and until now inert.
	// nil leaves the route returning 404.
	Webhook *WebhookTrigger
}

func (s *Server) swarmsDir() string {
	if s.SwarmsDir != "" {
		return s.SwarmsDir
	}
	return filepath.Join(filepath.Dir(s.BotsDir), "examples", "swarms")
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/status", s.handleStatus)
	mux.HandleFunc("POST /api/setup/oneclaw-key", s.handleSetupOneClawKey)
	mux.HandleFunc("GET /api/bots", s.handleListBots)
	mux.HandleFunc("POST /api/bots/{id}/services/{serviceId}/connection", s.handleSetBotServiceConnection)
	mux.HandleFunc("POST /api/bots/{id}/instructions", s.handleSetBotInstructions)
	mux.HandleFunc("GET /api/team", s.handleTeam)
	mux.HandleFunc("GET /api/roles", s.handleListRoles)
	mux.HandleFunc("POST /api/roles/{id}", s.handleSetRole)
	mux.HandleFunc("POST /api/roles/{id}/reset", s.handleResetRole)
	mux.HandleFunc("GET /api/swarms", s.handleListSwarms)
	mux.HandleFunc("GET /api/swarms/plan", s.handlePlan)
	mux.HandleFunc("GET /api/swarms/yaml", s.handleSwarmYAML)
	mux.HandleFunc("GET /api/swarms/full", s.handleGetSwarmFull)
	mux.HandleFunc("GET /api/swarms/export", s.handleExportSwarm)
	mux.HandleFunc("POST /api/swarms/import", s.handleImportSwarm)
	mux.HandleFunc("GET /api/swarms/{swarm}/webhook", s.handleWebhookDetails)
	mux.HandleFunc("POST /api/swarms/validate", s.handleValidateSwarm)
	mux.HandleFunc("POST /api/swarms", s.handleSaveSwarm)
	mux.HandleFunc("POST /api/compose", s.handleCompose)
	mux.HandleFunc("GET /api/spend", s.handleSpend)
	mux.HandleFunc("GET /api/connections", s.handleConnectionsStatus)
	mux.HandleFunc("POST /api/connections/google/start", s.handleConnectGoogleStart)
	mux.HandleFunc("POST /api/connections/x/start", s.handleConnectXStart)
	mux.HandleFunc("POST /api/connections/linkedin/start", s.handleConnectLinkedInStart)
	mux.HandleFunc("POST /api/connections/{service}", s.handleConnectToken)
	mux.HandleFunc("GET /api/connections/{service}/bots", s.handleProviderBots)
	mux.HandleFunc("POST /api/connections/{service}/bots", s.handleSetProviderConnection)
	mux.HandleFunc("POST /api/runs", s.handleStartRun)
	mux.HandleFunc("GET /api/runs", s.handleListRuns)
	mux.HandleFunc("GET /api/runs/{id}", s.handleGetRun)
	mux.HandleFunc("GET /api/runs/{id}/events", s.handleRunEvents)
	mux.HandleFunc("POST /api/runs/{id}/approvals/{approvalID}/decide", s.handleDecideApproval)
	mux.HandleFunc("GET /api/runs/{id}/fixtures", s.handleRunFixtures)
	mux.HandleFunc("POST /api/runs/{id}/fixtures", s.handlePinFixtures)
	mux.HandleFunc("POST /api/foundry", s.handleStartFoundryJob)
	mux.HandleFunc("GET /api/foundry", s.handleListFoundryJobs)
	mux.HandleFunc("GET /api/foundry/{id}", s.handleGetFoundryJob)
	mux.HandleFunc("GET /api/foundry/{id}/events", s.handleFoundryJobEvents)
	mux.HandleFunc("POST /api/foundry/{id}/approvals/{approvalID}/decide", s.handleDecideFoundryReview)
	mux.HandleFunc("GET /api/blobs/{uri}", s.handleGetBlob)

	// Token-authenticated, unlike everything above: this one spends money,
	// and Docker containers can reach it through host.docker.internal even
	// though nanobotd binds loopback.
	mux.HandleFunc("POST /shroud/{path...}", s.handleShroudProxy)
	// Also token-authenticated: this one starts runs, and runs send email.
	mux.HandleFunc("POST /webhooks/{swarm}", s.handleWebhook)

	mux.HandleFunc("POST /internal/steps/service_call", s.handleStepServiceCall)
	mux.HandleFunc("POST /internal/steps/ai_generate", s.handleStepAIGenerate)
	mux.HandleFunc("POST /internal/steps/memory_get", s.handleStepMemoryGet)
	mux.HandleFunc("POST /internal/steps/memory_put", s.handleStepMemoryPut)
	mux.HandleFunc("POST /internal/steps/memory_recall", s.handleStepMemoryRecall)
	mux.HandleFunc("POST /internal/steps/memory_remember", s.handleStepMemoryRemember)
	mux.HandleFunc("POST /internal/steps/approve", s.handleStepApprove)
	mux.HandleFunc("POST /internal/steps/notify", s.handleStepNotify)
	mux.HandleFunc("POST /internal/steps/web_fetch", s.handleStepWebFetch)

	return withCORS(mux)
}

// withCORS allows the WebUI dev server (a different origin during `vite
// dev`) to call nanobotd directly. Loopback-only in practice — nanobotd
// binds 127.0.0.1, not 0.0.0.0 (see cmd/nanobotd).
func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// vaultProbe caches the "is the 1Claw vault locked" check. Longer TTL than
// Docker's: unlocking involves a passkey prompt on another device, so
// nobody does it inside 20 seconds, and this one is a network round trip
// rather than a local command.
type vaultProbe struct {
	mu        sync.Mutex
	checkedAt time.Time
	locked    bool
	detail    string
}

const vaultProbeTTL = 20 * time.Second

func (p *vaultProbe) get(now time.Time, oc *oneclaw.Client, vaultID string) (bool, string) {
	if oc == nil || !oc.Configured() || vaultID == "" {
		return false, ""
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.checkedAt.IsZero() && now.Sub(p.checkedAt) < vaultProbeTTL {
		return p.locked, p.detail
	}
	p.locked, p.detail = oc.VaultLocked(vaultID)
	p.checkedAt = now
	return p.locked, p.detail
}

// dockerProbe caches runner.DockerAvailable for a few seconds. The status
// endpoint is polled by every open tab, and shelling out to `docker version`
// on each poll would be wasteful; a few seconds of staleness is invisible
// next to how long Docker Desktop takes to start anyway.
type dockerProbe struct {
	mu        sync.Mutex
	checkedAt time.Time
	ok        bool
	reason    string
}

const dockerProbeTTL = 5 * time.Second

func (p *dockerProbe) get(now time.Time) (bool, string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.checkedAt.IsZero() && now.Sub(p.checkedAt) < dockerProbeTTL {
		return p.ok, p.reason
	}
	p.ok, p.reason = runner.DockerAvailable()
	p.checkedAt = now
	return p.ok, p.reason
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	dockerOK, dockerReason := s.docker.get(now)
	memKind, memRecall := s.memoryStatus()
	llmKind, llmGuarded := s.llmStatus()
	vaultLocked, vaultReason := s.vault.get(now, s.OneClaw, s.VaultID)
	writeJSON(w, http.StatusOK, map[string]any{
		"oneclaw_configured": s.OneClaw != nil && s.OneClaw.Configured(),
		// A locked vault blocks every Slack/GitHub/Stripe/HubSpot bot at
		// once, and is fixed on the user's phone, not here.
		"vault_locked": vaultLocked,
		"vault_reason": vaultReason,
		// Which memory backend is behind memory.* steps, and whether it can
		// answer questions. A bot with a recall step works measurably worse
		// without one and degrades quietly by design, so the difference has
		// to be visible somewhere other than a run log — including *which*
		// bots it costs, read from the catalog so the answer can't go stale
		// as bots gain or lose recall steps.
		// Which LLM an ai.generate step reaches, and whether it carries
		// 1Claw's budget/redaction guardrails. Reported because the
		// difference is invisible in a run otherwise: a bot generates
		// either way, and only the bill and the redaction differ.
		"llm_backend":        llmKind,
		"llm_guardrails":     llmGuarded,
		"memory_backend":     memKind,
		"memory_recall":      memRecall,
		"memory_recall_bots": s.botsUsingRecall(),
		// Reported separately from oneclaw because they fail independently
		// and the fixes are unrelated: one is a key, the other is an app
		// you have to go start.
		"docker_available": dockerOK,
		"docker_reason":    dockerReason,
	})
}

// memoryStatus names the configured memory backend and says whether it can
// answer questions as well as store keys. See internal/memory.
func (s *Server) memoryStatus() (kind string, recall bool) {
	if s.Orchestrator == nil || s.Orchestrator.Memory == nil {
		return "none", false
	}
	store := s.Orchestrator.Memory
	_, recall = memory.RecallerOf(store)
	switch t := store.(type) {
	case *memory.Composite:
		if t.Rich != nil {
			return "local + " + recallerName(t.Rich), true
		}
		return "local", false
	case *memory.DeferredOneClaw:
		return "1claw", false
	case *memory.Local:
		return "local", false
	}
	return "custom", recall
}

// llmStatus names the configured LLM backend and says whether prompts pass
// through 1Claw's guardrails on the way out. See internal/llm.
func (s *Server) llmStatus() (kind string, guarded bool) {
	if s.Orchestrator == nil || s.Orchestrator.LLM == nil {
		return "none", false
	}
	g := s.Orchestrator.LLM
	return g.Describe(), llm.IsDeferredShroud(g)
}

// botsUsingRecall names the bots that ask memory a question, sorted, so
// Settings can say what a key/value backend is actually costing this
// install. Derived from the catalog rather than written into the UI: the
// list was three bots the day it was written and will not stay three.
func (s *Server) botsUsingRecall() []string {
	entries, err := os.ReadDir(s.BotsDir)
	if err != nil {
		return []string{}
	}
	var ids []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		nb, err := schema.LoadNanobot(filepath.Join(s.BotsDir, e.Name(), "nanobot.yaml"))
		if err != nil {
			continue
		}
		for _, st := range nb.Spec.Steps {
			if st.Type == "memory.recall" {
				ids = append(ids, e.Name())
				break
			}
		}
	}
	sort.Strings(ids)
	return nonNil(ids)
}

func recallerName(r memory.Recaller) string {
	if _, ok := r.(*memory.Honcho); ok {
		return "honcho"
	}
	return "recall"
}
