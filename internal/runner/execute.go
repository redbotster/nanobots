package runner

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"

	"github.com/redbotster/nanobots/internal/oneclaw"
	"github.com/redbotster/nanobots/internal/planner"
	"github.com/redbotster/nanobots/internal/schema"
	"github.com/redbotster/nanobots/internal/step"
)

// Orchestrator turns a planned Nanoswarm into real, sandboxed Docker
// containers — one per bot, in the planner's topological order — wiring
// each bot's inputs from swarm vars, upstream snaps, and port defaults.
type Orchestrator struct {
	RepoRoot      string // to build harness images and resolve bot dirs
	BotsDir       string
	CallbackAddr  string // how a container reaches nanobotd, e.g. http://host.docker.internal:7474
	Callbacks     *CallbackRegistry
	OneClaw       *oneclaw.Client // nil (or unconfigured) => every bot runs in demo mode
	AgentStateDir string
	RunWorkDir    string // per-run container workspaces live under here
	BlobDir       string // nanobotd's own persistent blob store
	Google        step.GoogleConfig
}

// ExecuteSwarm plans swarmPath, then runs it in the background, returning
// the Run immediately (status "running") so a caller can stream its log
// over SSE rather than blocking on the whole swarm.
func (o *Orchestrator) ExecuteSwarm(swarmPath string) (*Run, error) {
	result, err := planner.Plan(swarmPath, o.BotsDir)
	if err != nil {
		return nil, err
	}
	if !result.OK() {
		return nil, fmt.Errorf("swarm does not type-check:\n%s", result.Report())
	}
	order, err := result.DAG.TopoSort()
	if err != nil {
		return nil, err
	}

	run := NewRun(result.Resolved.Swarm.Metadata.Name)
	run.SetStatus(StatusRunning)

	go func() {
		for _, botID := range order {
			rb := result.Resolved.Bots[botID]
			if err := o.runBot(run, result.Resolved, botID, rb); err != nil {
				run.Log(botID, "", "FAILED: %v", err)
				run.SetStatus(StatusFailed)
				run.SetError(err)
				return
			}
		}
		run.SetStatus(StatusSucceeded)
	}()

	return run, nil
}

func (o *Orchestrator) runBot(run *Run, rs *planner.ResolvedSwarm, botID string, rb *planner.ResolvedBot) error {
	nb := rb.Nanobot
	run.Log(botID, "", "starting (%s harness)", nb.Spec.Harness.Type)

	harnessType := nb.Spec.Harness.Type
	if harnessType == "bare" && needsRealPDFRender(nb) {
		// A "bare" bot (catalog: render-pdf and anything built on it) whose
		// own job is producing a real PDF still needs Chrome to do that for
		// real — bare's distroless image doesn't have one. The harness type
		// stays "bare" in nanobot.yaml (that's the honest declaration: no
		// LLM loop, no dynamic reasoning), but execution borrows openclaw's
		// image so transform.render isn't silently degraded to HTML.
		harnessType = "openclaw"
		run.Log(botID, "", "using openclaw image for real Chrome rendering (bare harness has none)")
	}
	image, user, err := EnsureHarnessImage(harnessType, o.RepoRoot)
	if err != nil {
		return err
	}

	inputs, err := o.resolveInputs(run, rs, botID, rb)
	if err != nil {
		return fmt.Errorf("resolve inputs: %w", err)
	}

	runDir := filepath.Join(o.RunWorkDir, run.ID, botID)
	if err := os.MkdirAll(filepath.Join(runDir, "outputs"), 0o755); err != nil {
		return err
	}
	inputsJSON, err := json.Marshal(inputs)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(runDir, "inputs.json"), inputsJSON, 0o644); err != nil {
		return err
	}
	// swarm_vars.json is separate from inputs.json on purpose — the bot
	// contract (docs/bot-contract.md) keeps inputs.json a flat "one value
	// per declared input port" file; swarm-wide vars are a distinct,
	// optional thing a bot may reference via {{swarm.vars.*}} (see
	// bots/recap-emails-to-pdf/nanobot.yaml's upload step).
	if len(rs.Swarm.Spec.Vars) > 0 {
		varsJSON, err := json.Marshal(rs.Swarm.Spec.Vars)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(runDir, "swarm_vars.json"), varsJSON, 0o644); err != nil {
			return err
		}
	}

	var agentID, agentAPIKey string
	if o.OneClaw != nil && o.OneClaw.Configured() {
		agentID, agentAPIKey, err = o.OneClaw.EnsureAgent(o.AgentStateDir, "nanobots-"+nb.Metadata.Name, agentRequestFor(nb))
		if err != nil {
			return fmt.Errorf("ensure 1Claw agent: %w", err)
		}
	}

	blobs, err := step.NewFSBlobStore(o.BlobDir)
	if err != nil {
		return err
	}
	deps := BuildDeps(run, botID, nb, o.OneClaw, agentID, agentAPIKey, blobs, o.Google)

	token := uuid.NewString()
	o.Callbacks.Register(token, deps)
	defer o.Callbacks.Unregister(token)

	maxRuntime := time.Duration(nb.Spec.Guardrails.MaxRuntimeSecs) * time.Second
	exitCode, stderr, err := RunContainer(ContainerSpec{
		Image: image, User: user,
		BotDir: nb.SourcePath, RunDir: runDir,
		Env: map[string]string{
			"NANOBOTS_CALLBACK_URL": o.CallbackAddr,
			"NANOBOTS_RUN_TOKEN":    token,
			"NANOBOTS_BLOB_DIR":     "/tmp/nanobots-blobs",
		},
		MaxRuntime: maxRuntime,
	})

	replayContainerLog(run, botID, runDir)
	if err != nil {
		return fmt.Errorf("container exited %d: %w (%s)", exitCode, err, stderr)
	}

	outputs, err := collectOutputs(nb, blobs, filepath.Join(runDir, "outputs"))
	if err != nil {
		return fmt.Errorf("collect outputs: %w", err)
	}
	run.SetBotOutputs(botID, outputs)
	run.Log(botID, "", "done")
	return nil
}

// agentRequestFor derives a 1Claw agent creation request from a bot's
// declared model + guardrails, matching blueprint §3.2's compile step.
func agentRequestFor(nb *schema.Nanobot) oneclaw.CreateAgentRequest {
	g := nb.Spec.Guardrails
	return oneclaw.CreateAgentRequest{
		ShroudEnabled: true,
		MemoryEnabled: true,
		ShroudConfig: &oneclaw.ShroudConfig{
			PIIPolicy:             orDefault(g.PII, "redact"),
			InjectionThreshold:    orDefaultF(g.InjectionThreshold, 0.7),
			AllowedProviders:      []string{nb.Spec.Model.Provider},
			DailyBudgetUSD:        g.DailyBudgetUSD,
			EnableSecretRedaction: true,
		},
	}
}

func needsRealPDFRender(nb *schema.Nanobot) bool {
	for _, s := range nb.Spec.Steps {
		if s.Type == "transform.render" && s.To == "pdf" {
			return true
		}
	}
	return false
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func orDefaultF(f, def float64) float64 {
	if f == 0 {
		return def
	}
	return f
}

// replayContainerLog appends a bot's in-container step log (written by
// cmd/nanobot-agent to <runDir>/log.jsonl, one line per step regardless of
// type) to the run's aggregated log. This only happens once the container
// exits — the one step that's visible *while* a bot is still running is
// `approve`, because RunQueueApprover logs "awaiting approval" directly onto
// the run the moment the callback arrives, before the container is unblocked
// (see approver.go). Live per-step streaming for everything else is a
// reasonable future enhancement, not attempted here.
//
// Best-effort: a missing or unreadable log file isn't a run failure.
func replayContainerLog(run *Run, botID, runDir string) {
	f, err := os.Open(filepath.Join(runDir, "log.jsonl"))
	if err != nil {
		return
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	for {
		var line struct{ Step, Msg string }
		if err := dec.Decode(&line); err != nil {
			return
		}
		run.Log(botID, line.Step, "%s", line.Msg)
	}
}
