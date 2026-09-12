package runner

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"

	"github.com/redbotster/nanobots/internal/llm"
	"github.com/redbotster/nanobots/internal/memory"
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
	GitHub        step.GitHubConfig
	Slack         step.SlackConfig
	Stripe        step.StripeConfig
	HubSpot       step.HubSpotConfig
	X             step.XConfig
	LinkedIn      step.LinkedInConfig
	// Memory backs every memory.* step. See internal/memory.
	Memory memory.Store

	// LLM backs every ai.generate step — 1Claw Shroud, or a direct
	// provider key. Nil means this deployment has no LLM at all, and bots
	// fall back to their fixtures. See internal/llm.
	LLM llm.Generator
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
	run.SwarmPath = swarmPath
	run.SetStatus(StatusRunning)

	go func() {
		for _, botID := range order {
			rb := result.Resolved.Bots[botID]
			if err := o.runBot(run, result.Resolved, botID, rb); err != nil {
				run.Log(botID, "", "FAILED: %v", err)
				// SetError before SetStatus, not after: the terminal status
				// is what makes a run final, and RunStore snapshots it to
				// history right then. Setting the error afterwards persisted
				// failed runs with a blank "why", which is the one thing you
				// come back to a failed run for.
				run.SetError(err)
				run.SetStatus(StatusFailed)
				return
			}
		}
		run.SetStatus(StatusSucceeded)
	}()

	return run, nil
}

// runBot runs one bot instance — once, or once per item when a snap into it
// carries the fan-out marker (see planner/fanout.go).
//
// Fanning out is what "for each overdue invoice, send a reminder" has
// always meant and never done: six catalog swarms write `chaser.overdue.0`
// and carry a comment saying only the first item is handled.
func (o *Orchestrator) runBot(run *Run, rs *planner.ResolvedSwarm, botID string, rb *planner.ResolvedBot) error {
	fo, err := planner.FanOutFor(rs.Swarm, botID)
	if err != nil {
		return err
	}
	if fo == nil {
		return o.runBotOnce(run, rs, botID, rb, noFan, 1, nil)
	}

	n, err := o.fanOutWidth(run, fo)
	if err != nil {
		return err
	}
	if n == 0 {
		// Not an error: "for each overdue invoice" over no overdue invoices
		// is a successful no-op, and failing here would turn a quiet week
		// into a red run. Downstream still gets an empty list.
		run.Log(botID, "", "nothing to do — %s is empty", fo.Over)
		run.SetBotOutputs(botID, emptyListOutputs(rb.Nanobot))
		return nil
	}
	run.Log(botID, "", "running once per item — %d from %s", n, fo.Over)

	// One approval for the whole batch, not one per item (that decision is
	// the user's: twenty prompts means nobody reads them). BatchApprover
	// asks on the first iteration, naming the count, and reuses the answer
	// for the rest.
	batch := &BatchApprover{
		Inner: &RunQueueApprover{Run: run, Bot: botID, Step: "approve"},
		Total: n,
	}
	perItem := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		if err := o.runBotOnce(run, rs, botID, rb, fanIndex(i), n, batch); err != nil {
			return fmt.Errorf("item %d of %d: %w", i+1, n, err)
		}
		out, _ := run.BotOutputs(botID)
		perItem = append(perItem, out)
	}
	run.SetBotOutputs(botID, aggregateOutputs(rb.Nanobot, perItem))
	run.Log(botID, "", "done — %d item(s)", n)
	return nil
}

// fanOutWidth is how many items this bot will run for, and insists every
// marker-carrying snap agrees. Two lists of different lengths feeding one
// bot is a cross product, which is never what "for each" means.
func (o *Orchestrator) fanOutWidth(run *Run, fo *planner.FanOut) (int, error) {
	width := -1
	for _, snap := range fo.Snaps {
		n, err := o.fanOutLength(run, snap)
		if err != nil {
			return 0, err
		}
		if width >= 0 && n != width {
			return 0, fmt.Errorf(
				"fan-out inputs disagree: %s has %d item(s) but an earlier one has %d — they iterate together, so they must be the same length",
				snap.From, n, width)
		}
		width = n
	}
	return width, nil
}

// emptyListOutputs is what a bot that ran zero times produced: an empty list
// per declared output port, so a downstream bot sees "none" rather than a
// missing output it would fail on.
func emptyListOutputs(nb *schema.Nanobot) map[string]any {
	out := map[string]any{}
	for _, port := range nb.Spec.Ports.Outputs {
		out[port.Name] = []any{}
	}
	return out
}

// aggregateOutputs turns N runs' outputs into one list per port, matching
// what the planner promised downstream (see resolveEndpointType).
func aggregateOutputs(nb *schema.Nanobot, perItem []map[string]any) map[string]any {
	out := map[string]any{}
	for _, port := range nb.Spec.Ports.Outputs {
		vals := make([]any, 0, len(perItem))
		for _, item := range perItem {
			if v, ok := item[port.Name]; ok {
				vals = append(vals, v)
			}
		}
		out[port.Name] = vals
	}
	return out
}

func (o *Orchestrator) runBotOnce(run *Run, rs *planner.ResolvedSwarm, botID string, rb *planner.ResolvedBot, at fanIndex, total int, batch step.Approver) error {
	nb := rb.Nanobot
	if at == noFan {
		run.Log(botID, "", "starting (%s harness)", nb.Spec.Harness.Type)
	} else {
		run.Log(botID, "", "item %d of %d", int(at)+1, total)
	}

	harnessType := imageFor(nb, run, botID)
	image, user, err := EnsureHarnessImage(harnessType, o.RepoRoot)
	if err != nil {
		return err
	}

	inputs, err := o.resolveInputsAt(run, rs, botID, rb, at)
	if err != nil {
		return fmt.Errorf("resolve inputs: %w", err)
	}

	// Each item gets its own workspace, so one item's outputs can't be
	// mistaken for the next one's.
	runDir := filepath.Join(o.RunWorkDir, run.ID, botID)
	if at != noFan {
		runDir = filepath.Join(runDir, fmt.Sprintf("item-%d", int(at)))
	}
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

	// Only bots that actually use Shroud, memory, or a generic 1Claw service
	// binding get an agent — see agentneed.go for why "always" was wrong.
	var agentID, agentAPIKey string
	if o.OneClaw != nil && o.OneClaw.Configured() && needsOneClawAgent(nb) {
		agentID, agentAPIKey, err = o.OneClaw.EnsureAgent(o.AgentStateDir, "nanobots-"+nb.Metadata.Name, agentRequestFor(nb))
		if err != nil {
			return fmt.Errorf("ensure 1Claw agent: %w", err)
		}
	}

	blobs, err := step.NewFSBlobStore(o.BlobDir)
	if err != nil {
		return err
	}
	deps := BuildDeps(run, botID, nb, o.OneClaw, agentID, agentAPIKey, blobs, o.Google, o.GitHub, o.Slack, o.Stripe, o.HubSpot, o.X, o.LinkedIn, batch, o.memoryFor(agentID), o.LLM)

	token := uuid.NewString()
	o.Callbacks.Register(token, deps)
	defer o.Callbacks.Unregister(token)

	maxRuntime := time.Duration(nb.Spec.Guardrails.MaxRuntimeSecs) * time.Second
	// Exit code and stderr are both already inside RunContainer's error.
	_, _, err = RunContainer(ContainerSpec{
		Image: image, User: user,
		BotDir: nb.SourcePath, RunDir: runDir, BlobDir: o.BlobDir,
		Env: map[string]string{
			"NANOBOTS_CALLBACK_URL": o.CallbackAddr,
			"NANOBOTS_RUN_TOKEN":    token,
			"NANOBOTS_BLOB_DIR":     "/tmp/nanobots-blobs",
			// Where the host's own store is mounted, for reading files an
			// upstream bot produced. See step.FallbackBlobStore.
			"NANOBOTS_BLOB_READONLY_DIR": "/blobs",
		},
		MaxRuntime: maxRuntime,
	})

	replayContainerLog(run, botID, runDir)
	if err != nil {
		// RunContainer's error already carries the exit code and the
		// container's stderr. Re-wrapping produced "container exited 1:
		// container exited 1: <stderr> (<stderr>)" — the same text three
		// times in the one line the Runs page shows you.
		return err
	}

	outputs, err := collectOutputs(nb, blobs, filepath.Join(runDir, "outputs"))
	if err != nil {
		return fmt.Errorf("collect outputs: %w", err)
	}
	run.SetBotOutputs(botID, outputs)
	run.Log(botID, "", "done")
	return nil
}

// memoryFor resolves this bot's memory store. Everything is decided at
// startup except the 1Claw backend, which needs an agent id that only
// exists once the bot has one — so wiring leaves a marker and this fills it
// in. A bot with no agent (most of them, since only LLM bots get one) keeps
// whatever the marker was standing in for.
func (o *Orchestrator) memoryFor(agentID string) memory.Store {
	if o.Memory == nil {
		return nil
	}
	if agentID != "" && memory.IsDeferredOneClaw(o.Memory) && o.OneClaw != nil {
		return &memory.OneClaw{Client: o.OneClaw, AgentID: agentID}
	}
	return o.Memory
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

// needsBrowser reports whether any step actually drives Chrome.
//
// transform.render runs *in the container* (see step.RemoteDeps.Render — it
// calls RenderHTMLToPDF directly rather than calling back to nanobotd), so a
// bot that renders needs a real browser in its own image. Rendering to html
// does not.
//
// This used to check only `to: pdf`, which missed png — sheet-reporter
// renders a chart that way. It happened to work because that bot declares
// the openclaw harness anyway, but a bare bot rendering a png would have
// been handed an image with no Chrome in it.
func needsBrowser(nb *schema.Nanobot) bool {
	for _, s := range nb.Spec.Steps {
		if s.Type == "transform.render" && (s.To == "pdf" || s.To == "png") {
			return true
		}
	}
	return false
}

// imageFor picks the harness image from what a bot's steps actually need,
// and reports when that differs from what the bot declares.
//
// The vocabulary, and what each value costs at run time:
//
//	bare     fixed steps, no LLM, no browser        25MB
//	llm      fixed steps that call an LLM           25MB — the same image
//	openclaw needs a real browser                  1.1GB — Chromium
//
// llm and bare share an image on purpose: ai.generate is an HTTP callback
// to nanobotd, so the container never talks to a model and needs nothing
// beyond the interpreter and a CA bundle. Only transform.render to pdf/png
// needs a browser, and it needs one *in* the container
// (step.RemoteDeps.Render calls RenderHTMLToPDF directly rather than
// calling back).
//
// A mismatch between what a bot declares and what its steps need is
// resolved in favour of need, and logged. Both directions happen: a bare or
// llm bot that renders is promoted, and an openclaw bot that renders
// nothing drops to the small image rather than pulling 1.1GB to make an
// HTTP request.
func imageFor(nb *schema.Nanobot, run *Run, botID string) string {
	declared := nb.Spec.Harness.Type
	browser := needsBrowser(nb)

	if browser && declared != "openclaw" {
		run.Log(botID, "", "using the openclaw image: this bot renders a real PDF/PNG and needs Chrome")
		return "openclaw"
	}
	if !browser && declared == "openclaw" {
		run.Log(botID, "", "using the bare image: no step here needs a browser")
		return "bare"
	}
	return declared
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
