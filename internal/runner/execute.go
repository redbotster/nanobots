package runner

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/redbotster/nanobots/internal/llm"
	"github.com/redbotster/nanobots/internal/memory"
	"github.com/redbotster/nanobots/internal/oneclaw"
	"github.com/redbotster/nanobots/internal/planner"
	"github.com/redbotster/nanobots/internal/roles"
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
	// Services holds every connected-service credential in one value. See
	// step.ServiceConfigs.
	Services step.ServiceConfigs
	// Memory backs every memory.* step. See internal/memory.
	Memory memory.Store

	// Roles is the review-role library. A bot can read the live roster
	// through {{roles.roster}} in a port default, so a review board picks
	// from what the user has actually configured rather than inventing a
	// team from scratch each run. nil means no roster, and a board falls
	// back to inventing — see internal/roles.
	Roles *roles.Store

	// LLM backs every ai.generate step — 1Claw Shroud, or a direct
	// provider key. Nil means this deployment has no LLM at all, and bots
	// fall back to their fixtures. See internal/llm.
	LLM llm.Generator

	// runBotFn is the seam runLevels calls through, so its wave scheduling
	// and failure aggregation can be tested without Docker. nil means the
	// real thing; only tests set it.
	runBotFn func(*Run, *planner.ResolvedSwarm, string, *planner.ResolvedBot) error
}

// ExecuteSwarm plans swarmPath, then runs it in the background, returning
// the Run immediately (status "running") so a caller can stream its log
// over SSE rather than blocking on the whole swarm.
// ExecuteSwarmWithTrigger runs a swarm with a payload from whatever
// started it — today, a webhook. The payload reaches every bot's input
// templates as {{trigger.payload}}, alongside {{vars}} and {{run.date}}.
func (o *Orchestrator) ExecuteSwarmWithTrigger(swarmPath string, payload any) (*Run, error) {
	return o.executeSwarm(swarmPath, payload)
}

func (o *Orchestrator) ExecuteSwarm(swarmPath string) (*Run, error) {
	return o.executeSwarm(swarmPath, nil)
}

func (o *Orchestrator) executeSwarm(swarmPath string, triggerPayload any) (*Run, error) {
	result, err := planner.Plan(swarmPath, o.BotsDir)
	if err != nil {
		return nil, err
	}
	if !result.OK() {
		return nil, fmt.Errorf("swarm does not type-check:\n%s", result.Report())
	}
	levels, err := result.DAG.Levels()
	if err != nil {
		return nil, err
	}

	run := NewRun(result.Resolved.Swarm.Metadata.Name)
	run.SwarmPath = swarmPath
	run.TriggerPayload = triggerPayload
	run.SetStatus(StatusRunning)

	go func() {
		if err := o.runLevels(run, result.Resolved, levels); err != nil {
			// A run someone stopped reports that, not the wreckage of the
			// stopping. "2 bots in the same wave failed — meetings:
			// stopped; triage: stopped" is accurate and reads like
			// something went wrong; it did not, you asked.
			if run.WasStoppedByUser() {
				err = ErrStopped
			}
			// SetError before SetStatus, not after: the terminal status is
			// what makes a run final, and RunStore snapshots it to history
			// right then. Setting the error afterwards persisted failed
			// runs with a blank "why", which is the one thing you come back
			// to a failed run for.
			run.SetError(err)
			run.SetStatus(StatusFailed)
			return
		}
		run.SetStatus(StatusSucceeded)
	}()

	return run, nil
}

// maxParallelBots is the default cap on how many bots run at once within a
// wave.
//
// Each one is a container plus a model call, so the limit is about the
// machine rather than the model: four Chromium-bearing containers already
// want a couple of gigabytes, and a laptop that starts swapping finishes
// slower than it would have sequentially.
//
// Override with NANOBOTS_MAX_PARALLEL_BOTS. 1 restores the old strictly
// sequential behaviour, which is the honest way to compare — and the thing
// to reach for on a small machine, or when reading an interleaved run log
// is harder than waiting.
const maxParallelBots = 4

// parallelBots resolves the cap once per wave, so changing the environment
// takes effect on the next run rather than needing a restart.
func parallelBots() int {
	if v := os.Getenv("NANOBOTS_MAX_PARALLEL_BOTS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return maxParallelBots
}

// runLevels runs the swarm wave by wave, with the bots inside a wave
// running concurrently.
//
// A wave's bots have no path between them in the DAG, so nothing one
// produces can be read by another — which is what makes this safe rather
// than merely faster. Everything they do share is already synchronised: the
// run's log and outputs behind its mutex, the memory store behind its own,
// the callback registry behind its.
//
// On failure the wave is allowed to finish rather than being cancelled
// half-way. Two reasons. A bot that is mid-container would leave that
// container orphaned, which is the leak this runner already had once. And
// when two bots in a wave both fail, seeing both errors is more useful than
// seeing whichever lost the race — a swarm's two independent branches
// failing for one shared reason (an expired credential, say) is a common
// case, and reporting one of them sends you looking for two bugs.
func (o *Orchestrator) runLevels(run *Run, rs *planner.ResolvedSwarm, levels [][]string) error {
	limit := parallelBots()
	// dependsOn is who feeds whom, so a bot whose upstream never produced
	// anything is skipped rather than run against missing inputs. Without
	// this, one tolerated failure cascades into a confusing run of
	// "upstream bot has no recorded outputs yet" from every bot behind it.
	dependsOn := map[string][]string{}
	// A ResolvedSwarm always carries its Swarm in production; guarding is
	// for the malformed case, where a segfault is a much worse answer than
	// "no dependency edges and no error policy".
	snaps := []schema.Snap{}
	if rs.Swarm != nil {
		snaps = rs.Swarm.Spec.Snaps
	}
	for _, snap := range snaps {
		from, ferr := planner.ParseEndpoint(snap.From)
		to, terr := planner.ParseEndpoint(snap.To)
		if ferr == nil && terr == nil {
			dependsOn[to.BotID] = append(dependsOn[to.BotID], from.BotID)
		}
	}
	// Skipping is transitive for free: a skipped bot joins the set, so
	// anything behind it is skipped on the next wave too.
	gone := map[string]string{} // botID -> why its outputs never arrived

	for _, wave := range levels {
		var toRun []string
		for _, botID := range wave {
			if why, ok := upstreamMissing(dependsOn[botID], gone); ok {
				gone[botID] = fmt.Sprintf("skipped: %s", why)
				run.Log(botID, "", "skipped — %s", why)
				continue
			}
			toRun = append(toRun, botID)
		}
		if len(toRun) == 0 {
			continue
		}

		results := make(map[string]error, len(toRun))
		if len(toRun) == 1 || limit == 1 {
			// One at a time: either the wave has one bot (nine of the
			// fifteen catalog swarms are a straight chain), or the cap
			// says so.
			for _, botID := range toRun {
				results[botID] = o.runOneBot(run, rs, botID)
			}
		} else {
			run.Log("", "", "running %d bots at once: %s", len(toRun), strings.Join(toRun, ", "))
			var wg sync.WaitGroup
			var mu sync.Mutex
			slots := make(chan struct{}, limit)
			for _, botID := range toRun {
				botID := botID
				wg.Add(1)
				go func() {
					defer wg.Done()
					slots <- struct{}{}
					defer func() { <-slots }()
					err := o.runOneBot(run, rs, botID)
					mu.Lock()
					results[botID] = err
					mu.Unlock()
				}()
			}
			wg.Wait()
		}

		fatal := map[string]error{}
		for _, botID := range toRun {
			err := results[botID]
			if err == nil {
				continue
			}
			if onErrorContinue(rs, botID) {
				// The swarm said this bot's failure doesn't end the run.
				// Recorded rather than swallowed: a run that quietly stops
				// notifying anyone every night is the thing to avoid.
				run.Log(botID, "", "FAILED, but this swarm continues without it: %v", err)
				run.AddTolerated(botID, err.Error())
				gone[botID] = "the bot it needed failed, and this swarm was told to continue without it"
				continue
			}
			if errors.Is(err, ErrStopped) {
				// "FAILED: stopped from the app" contradicts itself. This
				// bot did not fail; it was cut short on purpose.
				run.Log(botID, "", "stopped")
			} else {
				run.Log(botID, "", "FAILED: %v", err)
			}
			fatal[botID] = err
		}
		if len(fatal) > 0 {
			return waveError(toRun, fatal)
		}
	}
	return nil
}

func (o *Orchestrator) runOneBot(run *Run, rs *planner.ResolvedSwarm, botID string) error {
	attempt := func() error {
		if o.runBotFn != nil {
			return o.runBotFn(run, rs, botID, rs.Bots[botID])
		}
		return o.runBot(run, rs, botID, rs.Bots[botID])
	}

	tries := retriesFor(rs, botID)
	var err error
	for i := 0; i <= tries; i++ {
		if i > 0 {
			run.Log(botID, "", "retrying (%d of %d) after: %v", i, tries, err)
		}
		err = attempt()
		if err == nil || !worthRetrying(err) {
			return err
		}
	}
	return err
}

// worthRetrying keeps a retry from turning a decision into a loop.
//
// A declined approval is the clearest case: asking again until someone says
// yes is not a retry, it is wearing them down. A run that ended underneath
// this bot is not retryable either — there is nothing left to run into.
func worthRetrying(err error) bool {
	msg := err.Error()
	for _, decision := range []string{
		"not approved",
		"the run ended before",
	} {
		if strings.Contains(msg, decision) {
			return false
		}
	}
	return true
}

// retriesFor reads the bot instance's declared retry count, and says
// something the first time it matters if the bot also writes somewhere.
func retriesFor(rs *planner.ResolvedSwarm, botID string) int {
	if rs.Swarm == nil {
		return 0
	}
	for _, b := range rs.Swarm.Spec.Bots {
		if b.ID == botID {
			if b.Retry < 0 {
				return 0
			}
			return b.Retry
		}
	}
	return 0
}

// upstreamMissing reports whether any bot this one reads from never
// produced outputs, and names the first such bot for the log.
func upstreamMissing(deps []string, gone map[string]string) (string, bool) {
	for _, d := range deps {
		if _, missing := gone[d]; missing {
			return fmt.Sprintf("%s never produced its outputs", d), true
		}
	}
	return "", false
}

// onErrorContinue reports whether the swarm marked this bot instance as
// non-fatal. Default is stop: a run that fails loudly is the safe reading
// of silence, and continuing has to be something someone chose.
func onErrorContinue(rs *planner.ResolvedSwarm, botID string) bool {
	if rs.Swarm == nil {
		return false
	}
	for _, b := range rs.Swarm.Spec.Bots {
		if b.ID == botID {
			return b.OnError == schema.OnErrorContinue
		}
	}
	return false
}

// waveError reports a wave's failures as one error, naming every bot that
// failed rather than only the first — in swarm order, so the message is the
// same whichever goroutine finished first.
func waveError(wave []string, failures map[string]error) error {
	var failed []string
	for _, botID := range wave {
		if err, ok := failures[botID]; ok {
			failed = append(failed, fmt.Sprintf("%s: %v", botID, err))
		}
	}
	if len(failed) == 1 {
		// Unchanged wording for the single-failure case, which is what
		// every existing message and test looks like.
		return errors.New(failed[0][strings.Index(failed[0], ": ")+2:])
	}
	return fmt.Errorf("%d bots in the same wave failed — %s", len(failed), strings.Join(failed, "; "))
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
		Inner: &RunQueueApprover{
			Run: run, Bot: botID, Step: "approve", OneClaw: o.OneClaw,
			Writes: rb.Nanobot.Spec.Guardrails.WritesAllowed,
			// Resolved when the gate actually opens rather than now: the
			// agent is per bot and this runs before the first item. Cheap
			// to call — EnsureAgent caches its existence check.
			AgentIDFn: func() string { id, _, _ := o.agentFor(rb.Nanobot); return id },
		},
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

	agentID, agentAPIKey, err := o.agentFor(nb)
	if err != nil {
		return err
	}

	blobs, err := step.NewFSBlobStore(o.BlobDir)
	if err != nil {
		return err
	}
	deps := BuildDeps(run, botID, nb, o.OneClaw, agentID, agentAPIKey, blobs, o.Services, batch, o.memoryFor(agentID), o.LLM)

	// Watch what this bot actually gets back, so a real run can be turned
	// into the bot's test data afterwards. Wrapping cannot change what the
	// bot sees — every method returns the inner value untouched — and only
	// the first item of a fan-out is recorded, since a fixture describes
	// one run of a bot and twenty would overwrite each other anyway.
	if at == noFan || at == 0 {
		rec := step.NewRecordingDeps(deps)
		deps = rec
		// Keyed by the catalog bot, not the swarm-local instance id: a
		// fixture belongs to `bots/review-board/`, not to whichever swarm
		// happened to call that instance "board".
		catalogID := catalogIDOf(rb)
		// A closure, not `defer run.SetCaptured(botID, rec.Captured())`:
		// deferred arguments are evaluated immediately, so that form
		// recorded an empty map before the bot had run at all.
		defer func() { run.SetCaptured(catalogID, rec.Captured()) }()
	}

	token := uuid.NewString()
	o.Callbacks.Register(token, deps)
	defer o.Callbacks.Unregister(token)

	maxRuntime := time.Duration(nb.Spec.Guardrails.MaxRuntimeSecs) * time.Second
	// Exit code and stderr are both already inside RunContainer's error.
	_, _, err = RunContainer(run.Context(), ContainerSpec{
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

// catalogIDOf maps a swarm-local bot instance to the bots/<id> directory it
// came from: "board" -> "review-board", from `use: review-board@0.1.0`.
func catalogIDOf(rb *planner.ResolvedBot) string {
	if rb == nil {
		return ""
	}
	if use := rb.Ref.Use; use != "" {
		if i := strings.Index(use, "@"); i >= 0 {
			return use[:i]
		}
		return use
	}
	// A local `path:` bot: the directory's own name is its id.
	return filepath.Base(strings.TrimRight(rb.Ref.Path, "/"))
}

// agentFor returns this bot's 1Claw agent, creating it if needed.
//
// Only bots that actually use Shroud, memory, or a generic 1Claw service
// binding get one — see agentneed.go for why "always" was wrong. Shared by
// the per-item run and the batch approver, which needs the same agent to
// mirror one approval for a whole fan-out.
func (o *Orchestrator) agentFor(nb *schema.Nanobot) (id, apiKey string, err error) {
	if o.OneClaw == nil || !o.OneClaw.Configured() || !needsOneClawAgent(nb) {
		return "", "", nil
	}
	id, apiKey, err = o.OneClaw.EnsureAgent(o.AgentStateDir, "nanobots-"+nb.Metadata.Name, agentRequestFor(nb))
	if err != nil {
		return "", "", fmt.Errorf("ensure 1Claw agent: %w", err)
	}
	return id, apiKey, nil
}
