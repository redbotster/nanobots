package runner

import (
	"context"
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
	"github.com/redbotster/nanobots/internal/secrets"
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
	// Secrets is where GitHub/Slack/Stripe/HubSpot's static tokens actually
	// live — a 1Claw vault, the OS keychain, or an encrypted local file.
	// nil means no backend at all, and every one of those providers fails
	// with "not configured" rather than reaching for a vault that isn't
	// there. See internal/secrets.
	Secrets secrets.Store
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

	// runBotFn is the seam runDAG calls through, so its scheduling and
	// failure aggregation can be tested without Docker. nil means the
	// real thing; only tests set it.
	runBotFn func(*Run, *planner.ResolvedSwarm, string, *planner.ResolvedBot) error

	// runBotOnceFn overrides runBotOnce when set. runBotFn's error-only
	// signature is enough for retry and fallback, which only care whether
	// an attempt succeeded, but loop:'s feed: and until: need the actual
	// output values a real attempt produced. nil means the real thing.
	runBotOnceFn func(*Run, *planner.ResolvedSwarm, string, *planner.ResolvedBot, fanIndex, int, step.Approver) (map[string]any, error)

	// retryWaitFn is the seam a retry backoff sleeps through, so a test can
	// see that the wait was requested without a real test taking up to a
	// minute to run. nil means the real thing: sleep, cancellable by the
	// run's own context.
	retryWaitFn func(context.Context, time.Duration)

	// vaultUnlockWaitFn is the same seam as retryWaitFn, for
	// attemptThroughVaultUnlock's own poll — a test can see the wait was
	// requested (and how many times) without sitting out 30 real seconds
	// per iteration.
	vaultUnlockWaitFn func(context.Context, time.Duration)

	// The one agent every approval is opened by. See approvalagent.go.
	approvalAgentState
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
	// result.OK() already proved result.DAG has no cycle (DAGErr is part of
	// that check) — runDAG needs the bots and their dependency edges, not
	// a precomputed wave grouping, so there's nothing further to compute
	// from the DAG here.

	run := NewRun(result.Resolved.Swarm.Metadata.Name)
	run.SwarmPath = swarmPath
	run.TriggerPayload = triggerPayload
	run.SetStatus(StatusRunning)

	go func() {
		if err := o.runDAG(run, result.Resolved); err != nil {
			// A run someone stopped reports that, not the wreckage of the
			// stopping. "2 bots failed — meetings: stopped; triage:
			// stopped" is accurate and reads like something went wrong; it
			// did not, you asked.
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

// maxParallelBots is the default cap on how many bots run at once,
// regardless of how many currently have their inputs ready.
//
// Each one is a container plus a model call, so the limit is about the
// machine rather than the model: four Chromium-bearing containers already
// want a couple of gigabytes, and a laptop that starts swapping finishes
// slower than it would have sequentially.
//
// Override with NANOBOTS_MAX_PARALLEL_BOTS. 1 restores strictly sequential
// behaviour, which is the honest way to compare — and the thing to reach
// for on a small machine, or when reading an interleaved run log is harder
// than waiting.
const maxParallelBots = 4

// parallelBots resolves the cap once per run, so changing the environment
// takes effect on the next run rather than needing a restart.
func parallelBots() int {
	if v := os.Getenv("NANOBOTS_MAX_PARALLEL_BOTS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return maxParallelBots
}

// runDAG starts each bot the moment its own upstream bots have finished,
// not when a precomputed wave boundary says to. Replaces the wave-by-wave
// scheduler this used to be (see git history for runLevels) — measured
// on a swarm with an uneven branch (docs/parallelism.md): a fast,
// independent bot no longer waits out a slow, unrelated one just because a
// topological sort happened to put them in the same numbered stage.
//
// Every bot gets its own goroutine immediately; each one's first act is to
// wait on the done-channels of the bots it actually reads from (dependsOn,
// below) — nothing else. Two bots with no path between them in the DAG can
// therefore start, run, and finish in either order or at the same time,
// which is what makes this safe rather than merely faster: nothing one
// produces can be read by another unless a snap says so, and everything
// they do share is already synchronised — the run's log and outputs behind
// its mutex, the memory store behind its own, the callback registry behind
// its.
//
// A fatal failure never cancels a bot that's already running, or one that
// hasn't started because it's still waiting on its own (unrelated)
// upstream — two reasons, both true before this rewrite and still true
// now. A bot mid-container would leave that container orphaned, which is
// the leak this runner already had once. And when two independent bots
// both fail, seeing both errors is more useful than seeing whichever lost
// the race — two branches failing for one shared reason (an expired
// credential, say) is a common case, and reporting one of them sends you
// looking for two bugs. A bot genuinely downstream of a fatal failure is
// still skipped, exactly as it always was: a fatal failure marks its own
// bot "gone" in the same map a tolerated failure or a quiet watch already
// used, so upstreamMissing catches it the same way — there is no separate
// "stop the run" flag to keep in sync with that map.
func (o *Orchestrator) runDAG(run *Run, rs *planner.ResolvedSwarm) error {
	limit := parallelBots()
	var botIDs []string
	if rs.Swarm != nil {
		for _, b := range rs.Swarm.Spec.Bots {
			botIDs = append(botIDs, b.ID)
		}
	}

	// dependsOn is who feeds whom, so a bot whose upstream never produced
	// anything is skipped rather than run against missing inputs, and so
	// each bot's goroutine knows exactly which other bots to wait for —
	// nothing else.
	dependsOn := map[string][]string{}
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

	// done[id] closes the moment that bot has finished, however it
	// finished — success, skip, tolerated failure, or fatal failure.
	// Waiting on a set of these is the entire scheduling mechanism: no
	// wave, no level, no precomputed stage.
	done := make(map[string]chan struct{}, len(botIDs))
	for _, id := range botIDs {
		done[id] = make(chan struct{})
	}

	var mu sync.Mutex // guards everything below
	gone := map[string]string{}
	fatal := map[string]error{}
	didWork := false
	quietReason := ""

	if len(botIDs) > 1 {
		run.Log("", "", "running %d bots, each starting as soon as its own inputs are ready", len(botIDs))
	}

	slots := make(chan struct{}, limit)
	var wg sync.WaitGroup
	for _, id := range botIDs {
		id := id
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer close(done[id])
			for _, dep := range dependsOn[id] {
				<-done[dep]
			}

			mu.Lock()
			why, missing := upstreamMissing(dependsOn[id], gone)
			mu.Unlock()
			if missing {
				// Inherited verbatim, not re-wrapped. Each hop used to add
				// its own prefix, so the third bot in a chain read
				// "skipped — notes: skipped: watcher: nothing to do: no
				// new file…". What everyone behind a stopped watch needs
				// to know is the same single fact, said once.
				mu.Lock()
				gone[id] = why
				mu.Unlock()
				run.Log(id, "", "skipped — %s", why)
				return
			}

			expr, skip, err := o.whenGate(run, rs, id)
			if err != nil {
				// The planner already proved when: only references this
				// bot's own declared inputs, so a live failure here means
				// something upstream of when: broke, not the condition
				// itself — treated exactly like the bot itself failing,
				// on_error: continue included.
				o.recordBotFailure(run, rs, id, err, &mu, gone, fatal)
				return
			}
			if skip {
				mu.Lock()
				gone[id] = fmt.Sprintf("%s: when: %s was false", id, expr)
				mu.Unlock()
				run.Log(id, "", "skipped — when: %s is false", expr)
				return
			}

			slots <- struct{}{}
			runErr := o.runOneBot(run, rs, id)
			<-slots

			if runErr == nil {
				mu.Lock()
				didWork = true
				mu.Unlock()
				return
			}
			var nothing *NothingToDoError
			if errors.As(runErr, &nothing) {
				// A watch that looked and found nothing new. Everything
				// downstream is skipped because its inputs genuinely never
				// arrived, and the run still succeeds — this is the
				// correct outcome of a watch, not a tolerated failure, and
				// an hourly one should read as quiet rather than as
				// twenty-four warnings.
				run.Log(id, "", "nothing to do — %s", nothing.Reason)
				mu.Lock()
				gone[id] = fmt.Sprintf("%s had nothing to do: %s", id, nothing.Reason)
				if quietReason == "" {
					quietReason = nothing.Reason
				}
				mu.Unlock()
				return
			}
			o.recordBotFailure(run, rs, id, runErr, &mu, gone, fatal)
		}()
	}
	wg.Wait()

	if len(fatal) > 0 {
		return dagError(botIDs, fatal)
	}
	if !didWork && quietReason != "" {
		run.SetNothingToDo(quietReason)
	}
	return nil
}

// recordBotFailure classifies one bot's failure — a swarm-declared
// on_error: continue, a stop, a decline, or a genuine fatal error — and
// records it under mu exactly once. Shared between runDAG's two failure
// sites (a when: evaluation error and the bot's own execution failing) so
// the classification can't drift between them.
func (o *Orchestrator) recordBotFailure(run *Run, rs *planner.ResolvedSwarm, botID string, err error, mu *sync.Mutex, gone map[string]string, fatal map[string]error) {
	if onErrorContinue(rs, botID) {
		// The swarm said this bot's failure doesn't end the run. Recorded
		// rather than swallowed: a run that quietly stops notifying anyone
		// every night is the thing to avoid.
		run.Log(botID, "", "FAILED, but this swarm continues without it: %v", err)
		run.AddTolerated(botID, err.Error())
		mu.Lock()
		gone[botID] = fmt.Sprintf("%s failed, and this swarm was told to continue without it", botID)
		mu.Unlock()
		return
	}
	switch {
	case errors.Is(err, ErrStopped):
		// "FAILED: stopped from the app" contradicts itself. This bot did
		// not fail; it was cut short on purpose.
		run.Log(botID, "", "stopped")
	case run.WasDeclinedByUser():
		// The same sentence one door along. The run page above this log
		// now says "declined" with a muted dot and explains that it
		// wasn't a fault — and the last line of the log underneath still
		// read `FAILED: ... not approved (decided_by=you)`, in red, about
		// the person's own answer.
		run.Log(botID, "", "declined: %v", err)
	default:
		run.Log(botID, "", "FAILED: %v", err)
	}
	mu.Lock()
	// A fatal failure marks itself "gone" too, same as a tolerated one or
	// a quiet watch — the wave-based version got this for free, because a
	// fatal failure always stopped the next wave from starting at all.
	// Without a wave boundary, a real downstream dependent needs this to
	// still see its upstream as missing and skip, rather than running
	// against outputs that were never produced.
	gone[botID] = fmt.Sprintf("%s failed", botID)
	fatal[botID] = err
	mu.Unlock()
}

func (o *Orchestrator) runOneBot(run *Run, rs *planner.ResolvedSwarm, botID string) error {
	err := o.runOneBotWithRetries(run, rs, botID)
	if err == nil || !worthRetrying(err) {
		// !worthRetrying also covers a declined approval and a stopped run —
		// neither is a failure a substitute bot can fix, so fallback doesn't
		// apply to them either.
		return err
	}
	fb, ok := rs.Fallbacks[botID]
	if !ok || run.Context().Err() != nil {
		return err
	}
	run.Log(botID, "", "falling back to %s after: %v", fb.Ref.Fallback, err)
	runFallback := o.runBot
	if o.runBotFn != nil {
		runFallback = o.runBotFn
	}
	if fbErr := runFallback(run, rs, botID, fb); fbErr != nil {
		return fmt.Errorf("%w (fallback %s also failed: %v)", err, fb.Ref.Fallback, fbErr)
	}
	run.Log(botID, "", "%s recovered using fallback %s", botID, fb.Ref.Fallback)
	return nil
}

func (o *Orchestrator) runOneBotWithRetries(run *Run, rs *planner.ResolvedSwarm, botID string) error {
	attempt := func() error {
		if o.runBotFn != nil {
			return o.runBotFn(run, rs, botID, rs.Bots[botID])
		}
		return o.runBot(run, rs, botID, rs.Bots[botID])
	}

	tries := retriesFor(rs, botID)
	backoff := retryBackoffFor(rs, botID)
	var err error
	for i := 0; i <= tries; i++ {
		if i > 0 {
			if backoff > 0 {
				run.Log(botID, "", "waiting %s before retrying (%d of %d) after: %v", backoff, i, tries, err)
				o.waitRetryBackoff(run, backoff)
			} else {
				run.Log(botID, "", "retrying (%d of %d) after: %v", i, tries, err)
			}
		}
		err = o.attemptThroughVaultUnlock(run, rs, botID, attempt)
		if err == nil || !worthRetrying(err) {
			return err
		}
	}
	return err
}

// vaultUnlockPollInterval is how often a bot stuck behind a passkey-locked
// vault is retried. Unmeasured against a real 1Claw account's own unlock
// latency — chosen as a reasonable middle ground between "notices within a
// minute of someone unlocking" and "doesn't hammer the API while nobody's
// looking."
const vaultUnlockPollInterval = 30 * time.Second

// attemptThroughVaultUnlock calls attempt, and if it fails because 1Claw's
// vault is passkey-locked (oneclaw.VaultLockedError), waits and retries
// indefinitely rather than counting against the bot's own retry budget
// (BotRef.Retry) or failing outright. A locked vault is not this bot's own
// transient failure — see docs/status.md's own disclosure of this — and
// could clear in a minute or the rest of the day; a hard failure here
// would be wrong the instant someone actually unlocks it, and burning a
// retry budget meant for real transient failures on "a human hasn't opened
// 1Claw yet" would exhaust it for no reason connected to the bot at all.
//
// Only run cancellation (the user stopping the run) ends the wait early —
// same shape as Run.RequestApproval, which also waits without a ceiling of
// its own until a human acts or the run itself ends.
//
// Refuses to retry at all when the bot declares guardrails.writes_allowed —
// the exact hazard docs/error-policy.md's CheckRetry already refuses at
// plan time for an ordinary retry:. A retry re-runs the whole bot; a bot
// that writes could have already sent something in an earlier step before
// a later step's vault read hit the lock, and retrying it would send that
// again. CheckRetry can't see this one coming (there's no retry: written
// down for it to catch), so the same guard has to live here instead: fail
// once, honestly, rather than silently risk a duplicate send.
func (o *Orchestrator) attemptThroughVaultUnlock(run *Run, rs *planner.ResolvedSwarm, botID string, attempt func() error) error {
	waited := false
	for {
		err := attempt()
		locked, ok := oneclaw.AsVaultLocked(err)
		if ok {
			if writes := writesAllowedFor(rs, botID); len(writes) > 0 {
				run.Log(botID, "", "%s — not retrying: this bot writes to %s, and a retry re-runs the whole bot",
					locked.Error(), strings.Join(writes, ", "))
				ok = false
			}
		}
		if !ok {
			if waited {
				// Same "only if still alive" guard Run.requestApproval uses
				// after its own wait: a container can outlast a human's
				// decision and fail the run on its own max_runtime_secs
				// while this loop was still waiting, and flipping a
				// terminal run back to "running" would leave on-disk
				// history and the live API disagreeing forever.
				if s := run.GetStatus(); s != StatusSucceeded && s != StatusFailed {
					run.SetStatus(StatusRunning)
				}
			}
			return err
		}
		waited = true
		run.SetStatus(StatusAwaitingUnlock)
		run.Log(botID, "", "%s — will retry automatically once unlocked", locked.Error())
		if stopErr := o.waitForVaultUnlockOrStop(run); stopErr != nil {
			return stopErr
		}
	}
}

// waitForVaultUnlockOrStop sleeps one poll interval, cut short if the run
// is cancelled — the same reason waitRetryBackoff does the same thing.
// Returns non-nil only when the run should give up rather than retry
// again.
func (o *Orchestrator) waitForVaultUnlockOrStop(run *Run) error {
	if o.vaultUnlockWaitFn != nil {
		o.vaultUnlockWaitFn(run.Context(), vaultUnlockPollInterval)
	} else {
		select {
		case <-time.After(vaultUnlockPollInterval):
		case <-run.Context().Done():
		}
	}
	if run.WasStoppedByUser() {
		return ErrStopped
	}
	return run.Context().Err()
}

// writesAllowedFor reads a bot instance's own declared
// guardrails.writes_allowed — the same field CheckRetry reads at plan time
// to refuse an ordinary retry: on a bot that writes.
func writesAllowedFor(rs *planner.ResolvedSwarm, botID string) []string {
	rb, ok := rs.Bots[botID]
	if !ok || rb.Nanobot == nil {
		return nil
	}
	return rb.Nanobot.Spec.Guardrails.WritesAllowed
}

// waitRetryBackoff sleeps out a retry's backoff, cut short if the run is
// cancelled — a stopped run should stop, not sit out the wait first.
func (o *Orchestrator) waitRetryBackoff(run *Run, d time.Duration) {
	if o.retryWaitFn != nil {
		o.retryWaitFn(run.Context(), d)
		return
	}
	select {
	case <-time.After(d):
	case <-run.Context().Done():
	}
}

// worthRetrying keeps a retry from turning a decision into a loop.
//
// A declined approval is the clearest case: asking again until someone says
// yes is not a retry, it is wearing them down. A run that ended underneath
// this bot is not retryable either — there is nothing left to run into.
func worthRetrying(err error) bool {
	msg := err.Error()
	var nothing *NothingToDoError
	if errors.As(err, &nothing) {
		// Retrying a watch that found nothing would poll the same folder
		// three times in a row and reach the same answer.
		return false
	}
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

// retryBackoffFor reads the bot instance's declared retry_backoff. An
// invalid value is the planner's job to have already refused — this parses
// defensively and treats anything it can't read as no backoff rather than
// failing a run over a wait, which is never the part someone cares about.
func retryBackoffFor(rs *planner.ResolvedSwarm, botID string) time.Duration {
	if rs.Swarm == nil {
		return 0
	}
	for _, b := range rs.Swarm.Spec.Bots {
		if b.ID != botID {
			continue
		}
		if b.RetryBackoff == "" {
			return 0
		}
		d, err := time.ParseDuration(b.RetryBackoff)
		if err != nil {
			return 0
		}
		return d
	}
	return 0
}

// upstreamMissing reports whether any bot this one reads from never
// produced outputs, and names the first such bot for the log.
func upstreamMissing(deps []string, gone map[string]string) (string, bool) {
	for _, d := range deps {
		if why, missing := gone[d]; missing {
			// The stored reason, not just the fact. "watcher never produced
			// its outputs" is true of a watch that found nothing new, of a
			// bot that failed under on_error: continue, and of a bot that
			// was itself skipped — three very different things, and the
			// difference is exactly what someone reading a quiet run wants.
			if why != "" {
				return why, true
			}
			return fmt.Sprintf("%s never produced its outputs", d), true
		}
	}
	return "", false
}

// whenGate evaluates a bot instance's when: condition, if it has one.
// Reports the raw expression (for the skip log) and whether it was false.
//
// Resolves this bot's inputs the same way runBot is about to — the
// planner's CheckWhen already proved when: can only reference a port this
// bot declares, so the condition sees exactly the same {{inputs.<port>}}
// values the bot's own steps would. Resolving twice (once here, once
// inside runBot) is the cost of that: simpler than threading the already-
// resolved map through the retry loop for a call that, per the planner's
// own guarantee, essentially never errors.
func (o *Orchestrator) whenGate(run *Run, rs *planner.ResolvedSwarm, botID string) (expr string, skip bool, err error) {
	if rs.Swarm == nil {
		return "", false, nil
	}
	for _, b := range rs.Swarm.Spec.Bots {
		if b.ID != botID {
			continue
		}
		if b.When == "" {
			return "", false, nil
		}
		rb, ok := rs.Bots[botID]
		if !ok {
			return b.When, false, nil
		}
		inputs, err := o.resolveInputsAt(run, rs, botID, rb, noFan)
		if err != nil {
			return b.When, false, fmt.Errorf("when: %q: %w", b.When, err)
		}
		pass, err := step.EvalCondition(b.When, map[string]any{"inputs": inputs})
		if err != nil {
			return b.When, false, fmt.Errorf("when: %w", err)
		}
		return b.When, !pass, nil
	}
	return "", false, nil
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

// dagError reports a whole run's fatal failures as one error, naming every
// bot that failed rather than only the first — in swarm order, so the
// message is the same whichever goroutine finished first, however far
// apart they actually finished in wall-clock time.
func dagError(botIDs []string, failures map[string]error) error {
	var failed []string
	for _, botID := range botIDs {
		if err, ok := failures[botID]; ok {
			failed = append(failed, fmt.Sprintf("%s: %v", botID, err))
		}
	}
	if len(failed) == 1 {
		// Unchanged wording for the single-failure case, which is what
		// every existing message and test looks like.
		return errors.New(failed[0][strings.Index(failed[0], ": ")+2:])
	}
	return fmt.Errorf("%d bots failed — %s", len(failed), strings.Join(failed, "; "))
}

// runItems runs a fanned-out bot's n items, at most limit at a time.
//
// Extracted so the awkward parts are testable without Docker, a model, or a
// swarm: that results stay paired with their index, that items *start* in
// order, and that a failure stops the ones that have not begun.
//
// Bounded, not unbounded. Twenty overdue invoices would otherwise be twenty
// concurrent model calls, and remedy.go already carries a rate-limit entry
// because that has been seen from a slower direction.
// NANOBOTS_MAX_PARALLEL_BOTS=1 restores the strictly sequential behaviour,
// which is the honest way to compare — and what to reach for on a small
// machine, or when reading an interleaved run log is harder than waiting.
func runItems(n, limit int, item func(i int) (map[string]any, error)) ([]map[string]any, []error) {
	out := make([]map[string]any, n)
	errs := make([]error, n)
	if limit > n {
		limit = n
	}
	if limit < 1 {
		limit = 1
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	failed := false
	slots := make(chan struct{}, limit)
	for i := 0; i < n; i++ {
		// The slot is taken here, in the loop, rather than inside the
		// goroutine. Spawning all n at once and letting them race for a slot
		// launches them in whatever order the scheduler feels like — item 3
		// before item 1, observed on the first real run of this — and at a
		// limit of 1 that is not the sequential behaviour the env var
		// promises.
		//
		// Acquiring here gives two guarantees, and deliberately not a third.
		// A limit of 1 is exactly the loop this replaced: one at a time, in
		// order. And at any limit, item i is not launched until i+1-limit
		// items have finished, so the run log stays roughly in order instead
		// of item 17 appearing before item 2. What it does *not* promise is
		// that item i's first instruction runs before item i+1's — once two
		// goroutines are live the scheduler decides, and a test asserting
		// otherwise failed on the first run for exactly that reason.
		slots <- struct{}{}
		// Stop starting items once one has failed. The sequential version
		// stopped dead on the first error, and a fanned-out bot is usually
		// sending something — so the fewer items that run after a failure,
		// the closer this stays to what it replaced. Items already in flight
		// are left to finish rather than cancelled: one mid-container would
		// leave that container orphaned, the same reason a wave is allowed
		// to finish.
		mu.Lock()
		stop := failed
		mu.Unlock()
		if stop {
			<-slots
			break
		}
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-slots }()
			o, err := item(i)
			mu.Lock()
			out[i], errs[i] = o, err
			if err != nil {
				failed = true
			}
			mu.Unlock()
		}()
	}
	wg.Wait()
	return out, errs
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
		loop := loopFor(rs, botID)
		if loop == nil {
			out, err := o.runBotOnce(run, rs, botID, rb, noFan, 1, nil)
			if err != nil {
				return err
			}
			run.SetBotOutputs(botID, out)
			return nil
		}
		out, err := o.runBotLoop(run, rs, botID, rb, loop)
		if err != nil {
			return err
		}
		run.SetBotOutputs(botID, out)
		return nil
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
			Run: run, Bot: botID, Step: "approve",
			Writes: rb.Nanobot.Spec.Guardrails.WritesAllowed,
			// Resolved when the gate actually opens rather than now: this
			// runs before the first fanned-out item, and a batch that is
			// never approved should not have created an agent.
			Mirror: o.approvalAgent,
		},
		Total: n,
	}
	// The items run concurrently, up to the same cap a wave uses.
	//
	// They are independent by definition — that is what fanning out means —
	// and it showed. Measured on a real supervisor-review run: three panel
	// reviews of the same work, 12.5s + 7.1s + 6.9s, one after another, for
	// 26.5s of a 50s run in which every single second was an ai.generate
	// call. Nothing about the second review depended on the first.
	//
	// What makes it safe was already true before this. Each item has had its
	// own `item-N` workspace since fan-out was written, container names are
	// UUIDs, and the run's log and outputs sit behind its mutex. The one
	// thing that was not safe is why runBotOnce returns its outputs now
	// rather than stashing them under the bot's name: twenty items writing
	// to one slot and reading it back is a race that would have paired the
	// wrong output with the wrong item, silently.
	perItem, errs := runItems(n, parallelBots(), func(i int) (map[string]any, error) {
		return o.runBotOnce(run, rs, botID, rb, fanIndex(i), n, batch)
	})

	// The lowest-numbered failure, so the message is the same one the
	// sequential version produced rather than whichever item lost a race.
	for i, err := range errs {
		if err != nil {
			return fmt.Errorf("item %d of %d: %w", i+1, n, err)
		}
	}
	run.SetBotOutputs(botID, aggregateOutputs(rb.Nanobot, perItem))
	run.Log(botID, "", "done — %d item(s)", n)
	return nil
}

// loopFor reads the bot instance's declared loop:, if any.
func loopFor(rs *planner.ResolvedSwarm, botID string) *schema.Loop {
	if rs.Swarm == nil {
		return nil
	}
	for _, b := range rs.Swarm.Spec.Bots {
		if b.ID == botID {
			return b.Loop
		}
	}
	return nil
}

// runBotLoop re-runs one bot instance in place, feeding its own previous
// output back as its own next input — pagination and polling: "keep
// fetching next_page until there isn't one, at most 20 times".
//
// Downstream sees exactly one of each declared output, the same as a bot
// that never looped: this is the deliberate difference from fan-out, whose
// outputs become lists. Accumulating anything *across* iterations (all the
// pages' items, not just the last page) is the bot's own job via
// memory.get/memory.put — the same primitive drive-watch already uses to
// remember across separate runs, used here to remember across iterations of
// one run instead. Keeping that here would mean this orchestrator inventing
// a second, competing idea of "accumulate", with no way to know which
// output on which bot was meant to grow versus which was meant to reset.
func (o *Orchestrator) runBotLoop(run *Run, rs *planner.ResolvedSwarm, botID string, rb *planner.ResolvedBot, loop *schema.Loop) (map[string]any, error) {
	iter := rb
	var out map[string]any
	for i := 0; i < loop.Max; i++ {
		run.Log(botID, "", "loop %d of %d", i+1, loop.Max)
		var err error
		out, err = o.runBotOnce(run, rs, botID, iter, noFan, 1, nil)
		if err != nil {
			return nil, err
		}
		if loop.Until != "" {
			stop, err := step.EvalCondition(loop.Until, map[string]any{"outputs": out})
			if err != nil {
				return nil, fmt.Errorf("loop.until: %w", err)
			}
			if stop {
				run.Log(botID, "", "loop stopped after %d of %d: %s is true", i+1, loop.Max, loop.Until)
				return out, nil
			}
		}
		if i == loop.Max-1 || len(loop.Feed) == 0 {
			break
		}
		fed := make(map[string]any, len(iter.Ref.Inputs)+len(loop.Feed))
		for k, v := range iter.Ref.Inputs {
			fed[k] = v
		}
		for inPort, outPort := range loop.Feed {
			fed[inPort] = out[outPort]
		}
		next := *iter
		next.Ref.Inputs = fed
		iter = &next
	}
	if loop.Until != "" {
		run.Log(botID, "", "loop reached its limit of %d without %s becoming true", loop.Max, loop.Until)
	}
	return out, nil
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

func (o *Orchestrator) runBotOnce(run *Run, rs *planner.ResolvedSwarm, botID string, rb *planner.ResolvedBot, at fanIndex, total int, batch step.Approver) (map[string]any, error) {
	if o.runBotOnceFn != nil {
		return o.runBotOnceFn(run, rs, botID, rb, at, total, batch)
	}
	nb := rb.Nanobot
	inProcess, whyContainer := runsInProcess(nb, rb.Ref.Execution)

	// One "starting" line, saying where it ran as well as which harness.
	// "Which of my bots skipped the sandbox" is a fair question to answer
	// from the log alone, and a second line for it would just be noise on
	// every run.
	if at == noFan {
		where := "in-process, no container"
		if !inProcess {
			where = "container: " + whyContainer
		}
		run.Log(botID, "", "starting (%s harness, %s)", nb.Spec.Harness.Type, where)
	} else {
		run.Log(botID, "", "item %d of %d", int(at)+1, total)
	}

	// Only touch Docker for a bot that actually needs it. EnsureHarnessImage
	// builds the image when it is missing, so doing this unconditionally
	// made Docker a hard dependency of every run — including runs of bots
	// that never open a container.
	var image, user string
	if !inProcess {
		harnessType := imageFor(nb, run, botID)
		var err error
		image, user, err = EnsureHarnessImage(harnessType, o.RepoRoot)
		if err != nil {
			return nil, err
		}
	}

	inputs, err := o.resolveInputsAt(run, rs, botID, rb, at)
	if err != nil {
		return nil, fmt.Errorf("resolve inputs: %w", err)
	}

	// Each item gets its own workspace, so one item's outputs can't be
	// mistaken for the next one's.
	runDir := filepath.Join(o.RunWorkDir, run.ID, botID)
	if at != noFan {
		runDir = filepath.Join(runDir, fmt.Sprintf("item-%d", int(at)))
	}
	if err := os.MkdirAll(filepath.Join(runDir, "outputs"), 0o755); err != nil {
		return nil, err
	}
	inputsJSON, err := json.Marshal(inputs)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(runDir, "inputs.json"), inputsJSON, 0o644); err != nil {
		return nil, err
	}
	// swarm_vars.json is separate from inputs.json on purpose — the bot
	// contract (docs/bot-contract.md) keeps inputs.json a flat "one value
	// per declared input port" file; swarm-wide vars are a distinct,
	// optional thing a bot may reference via {{swarm.vars.*}} (see
	// bots/recap-emails-to-pdf/nanobot.yaml's upload step).
	if len(rs.Swarm.Spec.Vars) > 0 {
		varsJSON, err := json.Marshal(rs.Swarm.Spec.Vars)
		if err != nil {
			return nil, err
		}
		if err := os.WriteFile(filepath.Join(runDir, "swarm_vars.json"), varsJSON, 0o644); err != nil {
			return nil, err
		}
	}

	agentID, agentAPIKey, err := o.agentFor(nb)
	if err != nil {
		return nil, err
	}

	blobs, err := step.NewFSBlobStore(o.BlobDir)
	if err != nil {
		return nil, err
	}
	deps := BuildDeps(run, botID, nb, o.OneClaw, agentID, agentAPIKey, blobs, o.Services, o.Secrets, batch, o.memoryFor(agentID), o.LLM, o.approvalAgent)

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

	if inProcess {
		// Said out loud, every run. "It went faster" is not a thing a user
		// should have to infer, and "which of my bots skipped the sandbox"
		// is a fair question to be able to answer from the log alone.
		if err := interpretInProcess(run, botID, nb, inputs, rs.Swarm.Spec.Vars, deps, blobs, runDir, maxRuntime); err != nil {
			return nil, describeTimeout(run, botID, maxRuntime, err)
		}
		outputs, err := collectOutputs(nb, blobs, filepath.Join(runDir, "outputs"))
		if err != nil {
			return nil, fmt.Errorf("collect outputs: %w", err)
		}
		run.Log(botID, "", "done")
		return outputs, nil
	}
	// Exit code and stderr are both already inside RunContainer's error.
	// Follow the bot's log while it runs, rather than replaying it after.
	// The agent flushes a line per step; this picks them up within a poll.
	tail := newLogTail(run, botID, runDir)
	stopTail := make(chan struct{})
	go tail.follow(stopTail)
	defer close(stopTail)

	noNetwork := !needsContainerNetwork(nb)
	if noNetwork {
		// Said out loud for the same reason "in-process, no container" is:
		// a sandbox nobody can see is a sandbox nobody trusts.
		run.Log(botID, "", "container has no network (this bot declares no egress and calls nothing)")
	}

	env := map[string]string{
		"NANOBOTS_CALLBACK_URL": o.CallbackAddr,
		"NANOBOTS_RUN_TOKEN":    token,
		"NANOBOTS_BLOB_DIR":     "/tmp/nanobots-blobs",
		// Where the host's own store is mounted, for reading files an
		// upstream bot produced. See step.FallbackBlobStore.
		"NANOBOTS_BLOB_READONLY_DIR": "/blobs",
	}
	// A bot's own callbacks (service.call, ai.generate, web.fetch, ...) are
	// already checked against guardrails.network_egress on the host — see
	// step.EgressPolicy. The one thing that check can't see is a real
	// Chrome, inside this same container, fetching whatever HTML from
	// transform.render happens to reference. This proxy closes that,
	// reusing the identical allowlist so the two can never disagree.
	egress, err := StartEgressProxyIfNeeded(nb.Spec.Guardrails.NetworkEgress, noNetwork)
	if err != nil {
		return nil, fmt.Errorf("start egress proxy: %w", err)
	}
	if egress != nil {
		defer egress.Close()
		env["NANOBOTS_EGRESS_PROXY"] = egress.Addr()
	}

	_, _, err = RunContainer(run.Context(), ContainerSpec{
		Image: image, User: user, NoNetwork: noNetwork,
		BotDir: nb.SourcePath, RunDir: runDir, BlobDir: o.BlobDir,
		Env:        env,
		MaxRuntime: maxRuntime,
	})

	tail.drain()
	if err != nil {
		// RunContainer's error already carries the exit code and the
		// container's stderr. Re-wrapping produced "container exited 1:
		// container exited 1: <stderr> (<stderr>)" — the same text three
		// times in the one line the Runs page shows you.
		return nil, describeTimeout(run, botID, maxRuntime, err)
	}

	outputs, err := collectOutputs(nb, blobs, filepath.Join(runDir, "outputs"))
	if err != nil {
		return nil, fmt.Errorf("collect outputs: %w", err)
	}
	run.Log(botID, "", "done")
	return outputs, nil
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
		Name:          agentNameFor(nb),
		Description:   agentDescriptionFor(nb),
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

// logTail follows a bot's in-container step log (written by
// cmd/nanobot-agent to <runDir>/log.jsonl, one line per step) and appends
// new lines to the run as they appear.
//
// This used to be a single replay after the container exited, and the
// product's own landing page said "every step streams to a run log in real
// time". Measured, a three-bot swarm sat on three log lines for sixteen
// seconds and then produced five at once: a bot's whole log arrived when it
// finished. The one exception was `approve`, which RunQueueApprover logs
// directly onto the run when the callback arrives.
//
// Reading the whole file each pass and emitting from `consumed` onward,
// rather than holding an offset: the files are a handful of lines, and a
// decoder that stops at the first error naturally ignores a line the agent
// is halfway through writing. That line is picked up on the next pass.
type logTail struct {
	run   *Run
	botID string
	path  string

	mu       sync.Mutex
	consumed int
}

func newLogTail(run *Run, botID, runDir string) *logTail {
	return &logTail{run: run, botID: botID, path: filepath.Join(runDir, "log.jsonl")}
}

// tailPoll is how often the log file is checked. Fast enough to read as
// live, slow enough that a bot doing real work is not competing with a
// stat loop.
const tailPoll = 300 * time.Millisecond

func (t *logTail) follow(stop <-chan struct{}) {
	ticker := time.NewTicker(tailPoll)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			t.drain()
		}
	}
}

// drain appends whatever is in the file beyond what has already been
// appended. Called on every poll and once more after the container exits,
// so a line written between the last poll and exit is never lost.
//
// Best-effort: a missing or unreadable log file isn't a run failure.
func (t *logTail) drain() {
	t.mu.Lock()
	defer t.mu.Unlock()

	f, err := os.Open(t.path)
	if err != nil {
		return
	}
	defer f.Close()

	var lines []struct{ Step, Msg string }
	dec := json.NewDecoder(f)
	for {
		var line struct{ Step, Msg string }
		if err := dec.Decode(&line); err != nil {
			break // EOF, or a line still being written
		}
		lines = append(lines, line)
	}
	for _, line := range lines[min(t.consumed, len(lines)):] {
		t.run.Log(t.botID, line.Step, "%s", line.Msg)
	}
	if len(lines) > t.consumed {
		t.consumed = len(lines)
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
	id, apiKey, err = o.OneClaw.EnsureAgent(o.AgentStateDir, agentNameFor(nb), agentRequestFor(nb))
	if err != nil {
		return "", "", fmt.Errorf("ensure 1Claw agent: %w", err)
	}
	return id, apiKey, nil
}

// describeTimeout renames a container timeout that was really a human not
// answering.
//
// Every one of the 54 runs on this machine that hit the 30-minute container
// cap was sitting on an unanswered approval — 27 hours of container time —
// and every one of them reported "container exceeded 30m0s and was
// stopped". That is true about the container and wrong about what happened:
//
//	05:00:19  [mailer] approve  awaiting approval: Send 'recap.pdf' to me@example.com?
//	05:30:19  [mailer] FAILED: container exceeded 30m0s and was stopped
//
// A 7am scheduled swarm asks a sleeping human to approve an email. Nobody
// answers. The Runs page then reports what reads as a hung bot, so the
// user goes looking for a bug in the bot instead of for the decision they
// missed — and the one action that would have fixed it, approving, is the
// one the message never mentions.
//
// Same class as "FAILED: stopped from the app" for a run the user stopped
// on purpose, and fixed the same way: say what actually happened.
func describeTimeout(run *Run, botID string, maxRuntime time.Duration, err error) error {
	if err == nil || !strings.Contains(err.Error(), "exceeded") {
		return err
	}
	pending := pendingFor(run, botID)
	if pending == nil {
		return err
	}
	waited := time.Since(pending.Created).Round(time.Second)
	// The fact, and only the fact.
	//
	// This carried its own advice at first — "approve it from the Runs page,
	// or take the approval off this step" — which put the same sentence
	// twice on the run page, once here and once in the remedy panel
	// underneath. Every other error in this app is a bare statement of what
	// happened, with web/src/lib/runError.ts supplying what to do about it,
	// a button to do it with, and a link to the doc. This one was the odd
	// one out because of how it was written, not because it needed to be.
	//
	// Same wording as the approval gate's own timeout in run.go, so the two
	// paths into this failure read the same way.
	return fmt.Errorf("nobody answered the approval %q within %s", pending.Summary, waited)
}

// pendingFor returns this bot's still-unanswered approval, if it has one.
func pendingFor(run *Run, botID string) *PendingApproval {
	if run == nil {
		return nil
	}
	for _, pa := range run.PendingApprovals() {
		if pa.Bot == botID {
			return pa
		}
	}
	return nil
}
