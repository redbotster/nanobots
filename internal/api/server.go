// Package api is nanobotd's REST+SSE surface: the WebUI's backend, and the
// callback target every bot container calls into (see
// internal/step.RemoteDeps and cmd/nanobot-agent). Go 1.22+'s http.ServeMux
// method+wildcard patterns are enough here — no router dependency needed,
// matching the blueprint's "keep the initial dependency list small" rule.
package api

import (
	"net/http"
	"path/filepath"

	"github.com/redbotster/nanobots/internal/foundry"
	"github.com/redbotster/nanobots/internal/oneclaw"
	"github.com/redbotster/nanobots/internal/runner"
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
	mux.HandleFunc("GET /api/swarms", s.handleListSwarms)
	mux.HandleFunc("GET /api/swarms/plan", s.handlePlan)
	mux.HandleFunc("GET /api/swarms/yaml", s.handleSwarmYAML)
	mux.HandleFunc("GET /api/swarms/full", s.handleGetSwarmFull)
	mux.HandleFunc("POST /api/swarms/validate", s.handleValidateSwarm)
	mux.HandleFunc("POST /api/swarms", s.handleSaveSwarm)
	mux.HandleFunc("POST /api/compose", s.handleCompose)
	mux.HandleFunc("GET /api/connections", s.handleConnectionsStatus)
	mux.HandleFunc("POST /api/connections/google/start", s.handleConnectGoogleStart)
	mux.HandleFunc("POST /api/connections/x/start", s.handleConnectXStart)
	mux.HandleFunc("POST /api/connections/linkedin/start", s.handleConnectLinkedInStart)
	mux.HandleFunc("POST /api/connections/{service}", s.handleConnectToken)
	mux.HandleFunc("POST /api/runs", s.handleStartRun)
	mux.HandleFunc("GET /api/runs", s.handleListRuns)
	mux.HandleFunc("GET /api/runs/{id}", s.handleGetRun)
	mux.HandleFunc("GET /api/runs/{id}/events", s.handleRunEvents)
	mux.HandleFunc("POST /api/runs/{id}/approvals/{approvalID}/decide", s.handleDecideApproval)
	mux.HandleFunc("POST /api/foundry", s.handleStartFoundryJob)
	mux.HandleFunc("GET /api/foundry", s.handleListFoundryJobs)
	mux.HandleFunc("GET /api/foundry/{id}", s.handleGetFoundryJob)
	mux.HandleFunc("GET /api/foundry/{id}/events", s.handleFoundryJobEvents)
	mux.HandleFunc("POST /api/foundry/{id}/approvals/{approvalID}/decide", s.handleDecideFoundryReview)
	mux.HandleFunc("GET /api/blobs/{uri}", s.handleGetBlob)

	mux.HandleFunc("POST /internal/steps/service_call", s.handleStepServiceCall)
	mux.HandleFunc("POST /internal/steps/ai_generate", s.handleStepAIGenerate)
	mux.HandleFunc("POST /internal/steps/memory_get", s.handleStepMemoryGet)
	mux.HandleFunc("POST /internal/steps/memory_put", s.handleStepMemoryPut)
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

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"oneclaw_configured": s.OneClaw != nil && s.OneClaw.Configured(),
	})
}
