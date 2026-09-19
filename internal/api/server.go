// Package api is nanobotd's REST+SSE surface: the WebUI's backend, and the
// callback target every bot container calls into (see
// internal/step.RemoteDeps and cmd/nanobot-agent). Go 1.22+'s http.ServeMux
// method+wildcard patterns are enough here — no router dependency needed,
// matching the blueprint's "keep the initial dependency list small" rule.
package api

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/redbotster/nanobots/internal/foundry"
	"github.com/redbotster/nanobots/internal/lab"
	"github.com/redbotster/nanobots/internal/llm"
	"github.com/redbotster/nanobots/internal/memory"
	"github.com/redbotster/nanobots/internal/oneclaw"
	"github.com/redbotster/nanobots/internal/roles"
	"github.com/redbotster/nanobots/internal/runner"
	"github.com/redbotster/nanobots/internal/scheduler"
	"github.com/redbotster/nanobots/internal/schema"
	"github.com/redbotster/nanobots/internal/secrets"
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

	// ScheduleBreaker is the scheduler's circuit breaker, shared so the
	// swarm list can report a paused schedule and POST .../schedule/resume
	// can clear it. nil means schedules are never paused — the behaviour
	// before it existed, and what a test gets by default.
	ScheduleBreaker *scheduler.Breaker

	// EnvFilePath is where handleSetupOneClawKey writes ONECLAW_API_KEY —
	// "" resolves to oneclaw.DefaultEnvFilePath(), the same file every
	// other credential-loading call in this build already reads from.
	EnvFilePath string

	// VaultID is the shared 1Claw vault every connected service's credential
	// lives in ("nanobots-main"), resolved once at startup by
	// internal/wiring. Empty means 1Claw isn't configured, and the lock
	// probe stays silent.
	VaultID string

	// Secrets is where handleConnectToken writes a pasted GitHub/Slack/
	// Stripe/HubSpot token and handleConnectionsStatus reads it back from —
	// a 1Claw vault, the OS keychain, or an encrypted local file, resolved
	// once at startup by internal/wiring.BuildSecretsStore. nil (only in
	// tests that don't set it) makes handleConnectToken refuse with a clear
	// error instead of a nil-pointer panic. See docs/secrets.md.
	Secrets secrets.Store

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

	// UI serves the built WebUI on any path the API does not claim, so
	// `nanobots up` is one command and one port. nil leaves "/" a 404,
	// which is what the daemon binary and every test get.
	UI http.Handler

	// Lab is the one ongoing Lab conversation this server holds — see
	// internal/lab and context/TEAM-LAB-DESIGN.md. nil leaves the Lab
	// endpoints reporting "not configured" rather than failing, the same
	// shape every other optional integration here uses.
	Lab *lab.Session

	// connCache holds the last /api/connections answer. Zero value is a
	// cold cache, so nothing has to construct it. See connections.go for
	// why an eight-round-trip read is worth caching at all.
	connCache ttlCache[[]connectionStatus]

	// postureCache holds the last /api/posture answer — three more 1Claw
	// round trips, on the Settings page. See posture.go.
	postureCache ttlCache[PostureResponse]

	// recallCache holds the list of bots with a memory.recall step, which
	// costs a read of every nanobot.yaml in the catalog. /api/status is
	// polled by every open tab; see recallBotsTTL.
	recallCache ttlCache[[]string]
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
	mux.HandleFunc("GET /api/schedule/describe", s.handleDescribeSchedule)
	mux.HandleFunc("GET /api/swarms/plan", s.handlePlan)
	mux.HandleFunc("GET /api/swarms/yaml", s.handleSwarmYAML)
	mux.HandleFunc("GET /api/swarms/full", s.handleGetSwarmFull)
	mux.HandleFunc("GET /api/swarms/export", s.handleExportSwarm)
	mux.HandleFunc("POST /api/swarms/import", s.handleImportSwarm)
	mux.HandleFunc("GET /api/swarms/{swarm}/webhook", s.handleWebhookDetails)
	mux.HandleFunc("POST /api/swarms/{swarm}/schedule/resume", s.handleResumeSchedule)
	mux.HandleFunc("POST /api/swarms/validate", s.handleValidateSwarm)
	mux.HandleFunc("POST /api/swarms", s.handleSaveSwarm)
	mux.HandleFunc("POST /api/compose", s.handleCompose)
	mux.HandleFunc("GET /api/spend", s.handleSpend)
	mux.HandleFunc("GET /api/posture", s.handlePosture)
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
	mux.HandleFunc("POST /api/runs/{id}/cancel", s.handleCancelRun)
	mux.HandleFunc("POST /api/runs/{id}/approvals/{approvalID}/decide", s.handleDecideApproval)
	mux.HandleFunc("GET /api/runs/{id}/fixtures", s.handleRunFixtures)
	mux.HandleFunc("POST /api/runs/{id}/fixtures", s.handlePinFixtures)
	mux.HandleFunc("POST /api/foundry", s.handleStartFoundryJob)
	mux.HandleFunc("GET /api/foundry", s.handleListFoundryJobs)
	mux.HandleFunc("GET /api/foundry/{id}", s.handleGetFoundryJob)
	mux.HandleFunc("GET /api/foundry/{id}/events", s.handleFoundryJobEvents)
	mux.HandleFunc("POST /api/foundry/{id}/approvals/{approvalID}/decide", s.handleDecideFoundryReview)
	mux.HandleFunc("POST /api/lab/messages", s.handleLabMessage)
	mux.HandleFunc("GET /api/lab/events", s.handleLabEvents)
	mux.HandleFunc("GET /api/blobs/{uri}", s.handleGetBlob)

	// Token-authenticated, unlike everything above: this one spends money,
	// and Docker containers can reach it through host.docker.internal even
	// though nanobotd binds loopback.
	mux.HandleFunc("POST /shroud/{path...}", s.handleShroudProxy)
	// Also token-authenticated: this one starts runs, and runs send email.
	mux.HandleFunc("POST /webhooks/{swarm}", s.handleWebhook)

	mux.HandleFunc("POST /internal/steps/service_call", s.handleStepServiceCall)
	mux.HandleFunc("POST /internal/steps/ai_generate", s.handleStepAIGenerate)
	mux.HandleFunc("POST /internal/steps/agent_generate", s.handleStepAgentGenerate)
	mux.HandleFunc("POST /internal/steps/memory_get", s.handleStepMemoryGet)
	mux.HandleFunc("POST /internal/steps/memory_put", s.handleStepMemoryPut)
	mux.HandleFunc("POST /internal/steps/memory_recall", s.handleStepMemoryRecall)
	mux.HandleFunc("POST /internal/steps/memory_remember", s.handleStepMemoryRemember)
	mux.HandleFunc("POST /internal/steps/approve", s.handleStepApprove)
	mux.HandleFunc("POST /internal/steps/notify", s.handleStepNotify)
	mux.HandleFunc("POST /internal/steps/web_fetch", s.handleStepWebFetch)

	// The WebUI, when one was built into this binary, on everything the API
	// did not claim. Registered last and on "/" so it cannot shadow a route:
	// http.ServeMux matches the most specific pattern, and every handler
	// above is more specific than "/".
	//
	// nil UI means the caller did not wire one — cmd/nanobotd, or a test —
	// and "/" stays unregistered so an unknown path 404s as it always did.
	if s.UI != nil {
		mux.Handle("/", s.UI)
	}

	return withCORS(mux)
}

// withCORS lets a locally-served WebUI call nanobotd, and nothing else.
//
// This used to answer `Access-Control-Allow-Origin: *` on every route,
// including POST. Binding loopback is no defence against that: the browser
// is already inside the loopback, so any page the user happened to have
// open could read every swarm and run, read a webhook token, start a run,
// save a swarm, and answer a pending approval — which is how a swarm sends
// mail or pays an invoice. The JSON content type makes those preflighted,
// and a wildcard passes the preflight.
//
// The stated reason for the wildcard was the `vite dev` server being a
// different origin. It isn't: vite proxies /api to this daemon server-side
// (web/vite.config.ts), every request the frontend makes is a relative
// path, and the browser therefore never makes a cross-origin request here
// at all. The wildcard bought the app nothing.
//
// Loopback origins are still echoed, so serving the built UI from any local
// port keeps working. A request with no Origin header is not a browser
// cross-origin request — curl, the vite proxy, a bot container — and gets
// no CORS headers because it needs none.
func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); origin != "" {
			if isLoopbackOrigin(origin) {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			}
			// Vary regardless of the decision: the response now depends on
			// the Origin, and these routes carry ETags. Without it a cache
			// could hand one origin's response to another.
			w.Header().Add("Vary", "Origin")
		}
		if r.Method == http.MethodOptions {
			// A disallowed origin gets 204 with no CORS headers, which the
			// browser reads as "not permitted" — the correct answer, and
			// one that leaks nothing about what exists here.
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// isLoopbackOrigin reports whether an Origin header names this machine.
//
// Parsed rather than prefix-matched: "http://127.0.0.1.evil.example" and
// "http://localhost@evil.example" both start with something that looks
// right, and a browser would send them from an attacker's page.
func isLoopbackOrigin(origin string) bool {
	u, err := url.Parse(origin)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	switch u.Hostname() {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	return false
}

// vaultProbe caches the "is the 1Claw vault locked" check. Longer TTL than
// Docker's: unlocking involves a passkey prompt on another device, so
// nobody does it inside 20 seconds, and this one is a network round trip
// rather than a local command.
//
// Both probes hold their mutex across the slow call rather than releasing
// it first, which makes them stampede-safe without a single-flight: a
// second caller arriving mid-check blocks, and then finds a value fresh
// enough to use, because the `now` it compares against was captured before
// it started waiting. That is why a browser page load showing two
// concurrent /api/status requests at 1.7s each was one doing the work and
// one waiting on it, not two doing it twice — unlike the ttlCache case in
// ttlcache.go, where releasing the lock first is what let four requests
// each pay full price.
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

	// The two probes are independent and both slow in different ways —
	// `docker version` is a 129ms shell-out, the vault check is a ~1.5s
	// network round trip to 1Claw. Measured on this machine; in sequence
	// they were the whole cost of this endpoint, and a browser page load
	// showed it at 1.7s.
	var (
		dockerOK                  bool
		dockerReason, vaultReason string
		vaultLocked               bool
		wg                        sync.WaitGroup
	)
	wg.Add(2)
	go func() { defer wg.Done(); dockerOK, dockerReason = s.docker.get(now) }()
	go func() {
		defer wg.Done()
		vaultLocked, vaultReason = s.vault.get(now, s.OneClaw, s.VaultID)
	}()

	memKind, memRecall := s.memoryStatus()
	llmKind, llmGuarded := s.llmStatus()
	recallBots := s.recallBots()
	wg.Wait()
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
		"memory_recall_bots": recallBots,
		// Which backend a pasted Slack/GitHub/Stripe/HubSpot token actually
		// lands in — a 1Claw vault, the OS keychain, or an encrypted local
		// file — and what that backend actually protects. Reported because
		// the difference is invisible otherwise: Settings' paste-a-token
		// form looks identical no matter which one answers it. See
		// docs/secrets.md.
		"secrets_backend": s.secretsStatus(),
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
		// The marker itself never implements Recaller — it's a stand-in
		// until a bot's agent id is known — so the RecallerOf(store) check
		// above always says false here regardless of the real backend.
		// memory.OneClaw does implement Recaller (real, lexical search —
		// see docs/1claw-feature-requests.md #12), so this used to be an
		// honest "no" and would now be a false one if left alone.
		return "1claw", true
	case *memory.SQLite:
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

// secretsStatus names the backend behind s.Secrets in the same words
// docs/secrets.md uses for it — what it is and what it actually protects,
// not just its name, since "1Claw vault" and "a file protected by your OS's
// own file permissions" are very different promises. "not configured" is
// only reachable in a test that never set Secrets; every real deployment
// gets one from internal/wiring.BuildSecretsStore.
func (s *Server) secretsStatus() string {
	if s.Secrets == nil {
		return "not configured"
	}
	return s.Secrets.Describe()
}

// botsUsingRecall names the bots that ask memory a question, sorted, so
// Settings can say what a key/value backend is actually costing this
// install. Derived from the catalog rather than written into the UI: the
// list was three bots the day it was written and will not stay three.
// recallBotsTTL bounds how stale the recall-bot list can be.
//
// The list comes from reading and parsing every nanobot.yaml in the
// catalog: 15ms and 39 files today, growing with the catalog. That was
// happening on every /api/status, which every open tab polls — so the cost
// was per-tab, per-poll, forever, to answer a question whose answer only
// changes when someone edits a bot file. Ten seconds is short enough that
// editing a bot and reloading shows the change, long enough that polling
// stops touching the disk.
const recallBotsTTL = 10 * time.Second

func (s *Server) recallBots() []string {
	ids, _ := s.recallCache.do(recallBotsTTL, func() ([]string, error) {
		return s.botsUsingRecall(), nil
	})
	return ids
}

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

// Warm fills the caches a page load would otherwise wait on, in the
// background, so the first one does not.
//
// /api/connections is 3.5s cold: eight vault secrets at roughly 900ms of
// round trip, concurrently, against a service that throttles concurrent
// reads. Four screens fetch it on mount, one of them the landing page's own
// getting-started card — so the app's very first screen opened by waiting
// on it. doStale removed the recurring cost at every TTL expiry, but the
// cold case has nothing to be stale with and correctly blocks.
//
// The daemon binds its port some seconds before a human has a browser
// pointed at it, and that gap is free. Failures are dropped on purpose:
// this is a prefetch of something the handler will ask for again, and a
// 1Claw that is down at startup should not be reported through a warm-up
// nobody asked for.
//
// Nothing here is required for correctness — the handlers work identically
// with a cold cache, which is what the tests run against.
func (s *Server) Warm() {
	if s.OneClaw == nil || !s.OneClaw.Configured() {
		return
	}
	go func() { _, _ = s.connCache.do(0, s.readConnections) }()
	go func() { _, _ = s.postureCache.do(0, s.readPosture) }()
}
