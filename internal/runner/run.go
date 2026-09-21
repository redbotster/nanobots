// Package runner is the local execution target: it turns a planned Nanoswarm
// into real Docker containers, one per bot in topological order, wiring each
// bot's declared inputs from swarm vars, upstream snaps, and defaults, and
// collecting its outputs back onto the context bus for the next bot. See
// NANOBOTS-BLUEPRINT.md §3.1 and §3.4.
package runner

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
)

type RunStatus string

const (
	StatusPending          RunStatus = "pending"
	StatusRunning          RunStatus = "running"
	StatusAwaitingApproval RunStatus = "awaiting_approval"
	// StatusAwaitingUnlock means a bot hit a passkey-locked 1Claw vault
	// (oneclaw.VaultLockedError) — not a failure and not a retry storm, the
	// same two things StatusAwaitingApproval already means for a pending
	// human decision. See Orchestrator.attemptThroughVaultUnlock.
	StatusAwaitingUnlock RunStatus = "awaiting_unlock"
	StatusSucceeded      RunStatus = "succeeded"
	StatusFailed         RunStatus = "failed"
)

// LogEntry is one line of a run's aggregated log, across all its bots.
type LogEntry struct {
	Time time.Time `json:"time"`
	Bot  string    `json:"bot"`
	Step string    `json:"step,omitempty"`
	Msg  string    `json:"msg"`
	// OpenSwarmPath is set only by Lab, on the entry announcing a swarm it
	// just composed and saved — never by a bot run or a foundry job, which
	// is why this lives on the shared LogEntry rather than a Lab-specific
	// type: internal/lab.Session already reuses *Run purely for its
	// existing Log/Subscribe/SSE plumbing (see lab.go's own doc comment),
	// and a second pub-sub mechanism just to carry one optional link back
	// to the WebUI would duplicate all of it for one field.
	OpenSwarmPath string `json:"open_swarm_path,omitempty"`
}

// PendingApproval is one `approve` step blocking on a human decision. The
// same mechanism is used whether the bot it belongs to is running against
// real 1Claw or demo fixtures — see docs/approvals.md for why approvals are
// one thing, not two.
type PendingApproval struct {
	ID       string    `json:"id"`
	Bot      string    `json:"bot"`
	Step     string    `json:"step"`
	Summary  string    `json:"summary"`
	RiskTier string    `json:"risk_tier"`
	Created  time.Time `json:"created"`
	// Writes is what this bot is allowed to touch outside the machine —
	// the bot's own guardrails.writes_allowed, carried here so the person
	// being asked can see the blast radius of "yes".
	//
	// The prompt used to read "Approval needed: Re: Export is failing on
	// large workspaces  [Skip] [Approve]", which says what the thing is
	// about and nothing about what approving does. A button that sends
	// mail should say it sends mail.
	Writes []string `json:"writes,omitempty"`

	decision chan approvalDecision
	// decided guards against a second answer to the same question. The
	// waiting goroutine removes an approval from the run's map, so between
	// Decide and that removal the approval is still listed as pending —
	// and anything that polls PendingApprovals in that window asks again.
	//
	// The CLI did exactly that: one log line arriving after an answer was
	// enough to re-prompt, hit EOF on a drained stdin, and record
	// "declined — nobody — no terminal attached to ask" on a run that had
	// just been approved. That second Decide also set DeclinedByUser on an
	// approved run, and would have blocked on this mutex holding the whole
	// run's lock if the waiter had not already drained the buffered
	// channel.
	decided bool
}

type approvalDecision struct {
	approved  bool
	decidedBy string
}

// Run is one execution of a swarm.
type Run struct {
	ID         string    `json:"id"`
	SwarmName  string    `json:"swarm_name"`
	Status     RunStatus `json:"status"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at,omitempty"`
	Error      string    `json:"error,omitempty"`
	// TriggeredBy is "manual" (a human clicked Run or invoked `nanobots
	// run`), "schedule" (internal/scheduler fired it), or "webhook"
	// (something posted to /webhooks/{swarm}) — set once at
	// construction/immediately after and never mutated again, so reading
	// it directly elsewhere carries the same no-further-writes safety as
	// ID/SwarmName (see GetStatus's own doc comment for the fields that
	// *do* need synchronized access). Exists so the Runs page can show a
	// run nobody clicked "why did this happen" instead of a mystery entry.
	TriggeredBy string `json:"triggered_by"`
	// StoppedByUser marks a run someone stopped on purpose. The status is
	// still "failed" — it did not finish, and inventing a sixth RunStatus
	// would mean auditing every switch, tone map and terminal check for a
	// distinction the error message already carries. But the scheduler's
	// circuit breaker has to know: five runs you stopped by hand are not a
	// swarm that is broken, and pausing its schedule over them would be the
	// app misreading you.
	StoppedByUser bool `json:"stopped_by_user,omitempty"`
	// DeclinedByUser marks a run that failed because a human answered "no"
	// to one of its approvals, through this app. Same argument as
	// StoppedByUser, and the same consumer: declining is the approval
	// feature working, and a swarm whose whole job is to ask before it
	// sends should not have its schedule paused for being told no.
	//
	// Found on a real machine: support-desk-lite had its schedule paused
	// with a streak that included declines the user had made deliberately,
	// while the app's own remedy for that error already said "This wasn't a
	// fault: the approval was declined." Two parts of one build disagreeing
	// about whether the user did something wrong.
	//
	// Set only by Decide, which is the REST path a person clicks. An
	// unattended decline — the CLI with no terminal to ask — never reaches
	// here, and should not: nobody chose that, and it will recur until
	// something changes.
	DeclinedByUser bool `json:"declined_by_user,omitempty"`
	// SwarmPath is the file this run was planned from — the one fact needed
	// to run the same thing again. Set once by ExecuteSwarm and never
	// mutated (same safety argument as TriggeredBy). Empty for a foundry
	// job, which embeds a Run but was never planned from a swarm file.
	SwarmPath string `json:"swarm_path,omitempty"`
	// TriggerPayload is whatever started this run carried — a webhook
	// body. Reaches every bot's input templates as {{trigger.payload}}.
	// Set once at construction and never mutated, like SwarmPath.
	TriggerPayload any `json:"trigger_payload,omitempty"`
	// NothingToDo is why a run did no work: a watch looked and found
	// nothing new, so every bot either stopped or was skipped behind one.
	//
	// Not a status. The run genuinely succeeded — it did exactly what a
	// watch is for — and calling it anything else would make an hourly
	// schedule look broken. But "succeeded" alone, on twenty-four rows a
	// day, hides the one run that actually did something, which is the
	// only row anybody is looking for.
	NothingToDo string `json:"nothing_to_do,omitempty"`

	// Tolerated are bots that failed while the swarm was told to continue
	// without them. Guarded by mu — read it with GetTolerated.
	Tolerated []ToleratedFailure `json:"tolerated,omitempty"`

	mu            sync.Mutex
	captured      map[string]map[string]any // botID -> fixture filename -> value
	demoServices  map[string]bool           // "<bot>.<service>" served from fixtures
	log           []LogEntry
	outputs       map[string]map[string]any // botID -> port -> value (JSON-safe)
	approvals     map[string]*PendingApproval
	subscribers   map[chan LogEntry]bool
	onTerminalFns []func(*Run)
	// onChangeFns fire on every mutation that changes a field the runs-list
	// summary carries (status, error, stopped/declined, nothing-to-do, the
	// pending approval count) — registered once by RunStore.Add so the
	// runs-list SSE stream (internal/api.handleRunsEvents) can push a delta
	// the moment it happens, instead of every subscriber discovering it up
	// to two seconds later on the next poll.
	onChangeFns []func(*Run)
	// ctx/cancel are the run's lifetime — see Context and Stop.
	ctx    context.Context
	cancel context.CancelFunc
}

func NewRun(swarmName string) *Run {
	// Every run carries its own cancellable lifetime, so Stop has something
	// to pull. Background rather than a request's context on purpose: a run
	// outlives the HTTP call that started it, and tying it to that would end
	// the run the moment the browser tab navigated away.
	ctx, cancel := context.WithCancel(context.Background())
	return &Run{
		ID:          uuid.NewString(),
		SwarmName:   swarmName,
		TriggeredBy: "manual",
		Status:      StatusPending,
		StartedAt:   time.Now(),
		outputs:     map[string]map[string]any{},
		approvals:   map[string]*PendingApproval{},
		subscribers: map[chan LogEntry]bool{},
		ctx:         ctx,
		cancel:      cancel,
	}
}

// Context is the run's lifetime. Everything that can block — a container, a
// step callback — should take it, so Stop actually stops rather than
// politely asking and waiting out the ceiling.
func (r *Run) Context() context.Context {
	if r.ctx == nil {
		// A Run built as a bare struct literal (some tests do) has no
		// context. Never nil to a caller: a nil context panics deep inside
		// exec, a long way from the line that forgot to use the constructor.
		return context.Background()
	}
	return r.ctx
}

// Stop asks the run to end now. Idempotent, and safe to call on a run that
// has already finished — the second Stop of a run that is already stopping
// is a double-click, not an error.
//
// Returns false when there was nothing to stop, so the API can answer 409
// rather than pretending it did something.
func (r *Run) Stop() bool {
	r.mu.Lock()
	if r.Status == StatusSucceeded || r.Status == StatusFailed {
		r.mu.Unlock()
		return false
	}
	r.StoppedByUser = true
	cancel := r.cancel
	r.mu.Unlock()

	r.Log("", "", "stopping — asked from the app")
	if cancel != nil {
		cancel()
	}
	// An approval gate is not waiting on the context, it is waiting on a
	// human. Releasing those is what lets a run parked at a gate actually
	// end rather than sit there until the approval times out.
	r.releaseApprovals()
	r.notifyChanged()
	return true
}

// WasStoppedByUser reports whether someone stopped this run on purpose.
func (r *Run) WasStoppedByUser() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.StoppedByUser
}

func (r *Run) Log(bot, step, format string, a ...any) {
	r.appendEntry(LogEntry{Time: time.Now(), Bot: bot, Step: step, Msg: fmt.Sprintf(format, a...)})
}

// LogOpenSwarm is Log plus a link — used exactly once, by Lab announcing a
// swarm it just composed and saved, so the WebUI can render that one
// entry with something to click instead of a bare path in prose.
func (r *Run) LogOpenSwarm(bot, swarmPath, format string, a ...any) {
	r.appendEntry(LogEntry{
		Time: time.Now(), Bot: bot, Msg: fmt.Sprintf(format, a...), OpenSwarmPath: swarmPath,
	})
}

func (r *Run) appendEntry(entry LogEntry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.log = append(r.log, entry)
	// Sent while still holding r.mu, deliberately. This used to snapshot the
	// subscribers, unlock, and then send — which raced with Unsubscribe's
	// close() and panicked with "send on closed channel". The select's
	// default guards a *full* channel, not a *closed* one. And because
	// ExecuteSwarm runs a swarm in a detached goroutine, that panic wasn't
	// caught by net/http's per-connection recover: it took down the whole
	// daemon and lost the run. Closing a tab on a running swarm was enough
	// to trigger it.
	//
	// Holding the lock here is cheap: every send is non-blocking, and the
	// subscriber count is the number of open SSE streams.
	for ch := range r.subscribers {
		select {
		case ch <- entry:
		default: // a slow subscriber never blocks the run
		}
	}
}

// Subscribe returns a channel of log entries as they're appended, for an SSE
// handler to stream. Call Unsubscribe when the client disconnects.
func (r *Run) Subscribe() chan LogEntry {
	ch := make(chan LogEntry, 64)
	r.mu.Lock()
	r.subscribers[ch] = true
	r.mu.Unlock()
	return ch
}

func (r *Run) Unsubscribe(ch chan LogEntry) {
	// close() under the same lock Log sends under — the two must never
	// interleave. See Log.
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.subscribers[ch]; !ok {
		return // already unsubscribed; closing twice would panic too
	}
	delete(r.subscribers, ch)
	close(ch)
}

func (r *Run) LogEntries() []LogEntry {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]LogEntry, len(r.log))
	copy(out, r.log)
	return out
}

func (r *Run) SetStatus(s RunStatus) {
	r.mu.Lock()
	r.Status = s
	terminal := s == StatusSucceeded || s == StatusFailed
	if terminal {
		r.FinishedAt = time.Now()
	}
	fns := r.onTerminalFns
	if terminal {
		// Whatever was waiting on a human is unreachable now — its
		// container is gone. Leaving these listed kept a dead run showing
		// "1 approval waiting" in the nav badge and the Runs page.
		for id, pa := range r.approvals {
			delete(r.approvals, id)
			close(pa.decision)
		}
	}
	r.mu.Unlock()

	// Outside the lock: a callback that reads the run (RunStore's does, to
	// snapshot it) would deadlock on r.mu otherwise.
	if terminal {
		for _, fn := range fns {
			fn(r)
		}
	}
	r.notifyChanged()
}

// onTerminal registers fn to run once this run succeeds or fails. Used by
// RunStore to write history at the only moment a run is worth writing: when
// it's complete and will never change again.
func (r *Run) onTerminal(fn func(*Run)) {
	r.mu.Lock()
	already := r.Status == StatusSucceeded || r.Status == StatusFailed
	if !already {
		r.onTerminalFns = append(r.onTerminalFns, fn)
	}
	r.mu.Unlock()
	// A run can finish before anyone registers (a swarm that fails during
	// planning is already failed by the time the store sees it) — fire
	// immediately rather than silently never firing.
	if already {
		fn(r)
	}
}

// onChange registers fn to run on every subsequent change to a field the
// runs-list summary carries. Unlike onTerminal there's no "already true"
// case to fire immediately for: RunStore.Add calls this once, right after
// constructing the run, before anything has had a chance to change.
func (r *Run) onChange(fn func(*Run)) {
	r.mu.Lock()
	r.onChangeFns = append(r.onChangeFns, fn)
	r.mu.Unlock()
}

func (r *Run) notifyChanged() {
	r.mu.Lock()
	fns := r.onChangeFns
	r.mu.Unlock()
	for _, fn := range fns {
		fn(r)
	}
}

func (r *Run) SetError(err error) {
	r.mu.Lock()
	r.Error = err.Error()
	// Callers should set the error before the terminal status (execute.go
	// says why), but one that doesn't shouldn't silently persist a failed
	// run with a blank "why" — re-fire so history catches up. Writing the
	// same snapshot twice is harmless; losing the reason isn't.
	var fns []func(*Run)
	if r.Status == StatusSucceeded || r.Status == StatusFailed {
		fns = r.onTerminalFns
	}
	r.mu.Unlock()

	for _, fn := range fns {
		fn(r)
	}
	r.notifyChanged()
}

// GetStatus/GetError/GetFinishedAt are synchronized reads of the fields
// SetStatus/SetError mutate under r.mu — found necessary (not
// hypothetical: caught by go test -race) once a second consumer besides
// this run's own goroutine started polling a live run's JSON shape
// (internal/foundry's Job embeds *Run and is polled the same way an
// in-progress swarm run already was via runToJSON). ID/SwarmName/StartedAt
// are write-once at NewRun and never mutated again, so reading them
// directly elsewhere isn't a race the same way.
// ToleratedFailure is a bot that failed while the swarm was told to carry
// on without it (BotRef.OnError: continue).
//
// Recorded rather than swallowed, and rather than given its own terminal
// status. A third status would have to be understood by the run store, the
// scheduler, every filter and every tone map — and the honest summary is
// still "the run finished": the reminders went out, the Slack message
// didn't. What matters is that it cannot be invisible, so a run carrying
// one of these renders as a warning wherever a plain success would render
// as green.
type ToleratedFailure struct {
	Bot   string `json:"bot"`
	Error string `json:"error"`
}

// NoteDemoService records that a bot reached a service through fixtures
// rather than a real account.
//
// A run made entirely of demo data succeeds, produces plausible emails and
// invoices, and is indistinguishable from a real one — and that is the
// *default*, since every bot ships on `connection: demo`. The log now says
// so per call; this is the same fact where someone reading a result will
// actually meet it.
func (r *Run) NoteDemoService(bot, service string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.demoServices == nil {
		r.demoServices = map[string]bool{}
	}
	r.demoServices[bot+"."+service] = true
}

// DemoServices lists "<bot>.<service>" pairs served from fixtures, sorted.
func (r *Run) DemoServices() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.demoServices))
	for k := range r.demoServices {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// SetCaptured stores what a bot's real run would produce as fixtures.
//
// Kept on the run rather than written straight to disk: turning a run into
// a bot's committed test data is a deliberate act, and one that overwrites
// files in the repo. The run holds the material; a person decides.
func (r *Run) SetCaptured(botID string, fixtures map[string]any) {
	if len(fixtures) == 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.captured == nil {
		r.captured = map[string]map[string]any{}
	}
	r.captured[botID] = fixtures
}

// Captured returns what each bot's run would produce as fixtures.
func (r *Run) Captured() map[string]map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make(map[string]map[string]any, len(r.captured))
	for bot, fx := range r.captured {
		copyFx := make(map[string]any, len(fx))
		for k, v := range fx {
			copyFx[k] = v
		}
		out[bot] = copyFx
	}
	return out
}

// AddTolerated records a failure the swarm chose to continue past.
func (r *Run) AddTolerated(bot, msg string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Tolerated = append(r.Tolerated, ToleratedFailure{Bot: bot, Error: msg})
}

// SetNothingToDo records that this run found nothing to do, and why.
func (r *Run) SetNothingToDo(reason string) {
	r.mu.Lock()
	r.NothingToDo = reason
	r.mu.Unlock()
	r.notifyChanged()
}

// GetNothingToDo returns why this run did no work, or "" if it did some.
func (r *Run) GetNothingToDo() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.NothingToDo
}

// Outcome is the word for what happened, rather than the status.
//
// Three runs end `failed` and only one of them is a fault: a run someone
// stopped, a run whose approval someone declined, and a run that actually
// broke. A fourth ends `succeeded` having deliberately done nothing. The
// status stays as it is — inventing more of them would mean auditing every
// switch and terminal check in the repo for a distinction the booleans
// already carry — but nothing user-facing should print the status raw.
//
// `nanobots run` printed `run <id>: failed` for all three, so declining
// your own approval in the terminal ended with the word "failed" and no
// hint that you had caused it on purpose. The WebUI had made this
// distinction for stopped runs for a while, which is what made the CLI's
// silence on it a drift rather than an omission.
//
// The WebUI still renders these from the booleans rather than calling this,
// because it also picks a dot tone and a layout per case. The booleans are
// the one fact; the word is a presentation choice each surface makes.
func (r *Run) Outcome() string {
	switch {
	case r.WasStoppedByUser():
		return "stopped"
	case r.WasDeclinedByUser():
		return "declined"
	case r.GetNothingToDo() != "":
		return "nothing to do"
	}
	return string(r.GetStatus())
}

// GetTolerated returns the failures this run continued past.
func (r *Run) GetTolerated() []ToleratedFailure {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]ToleratedFailure, len(r.Tolerated))
	copy(out, r.Tolerated)
	return out
}

func (r *Run) GetStatus() RunStatus {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.Status
}

func (r *Run) GetError() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.Error
}

func (r *Run) GetFinishedAt() time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.FinishedAt
}

func (r *Run) SetBotOutputs(botID string, outputs map[string]any) {
	r.mu.Lock()
	r.outputs[botID] = outputs
	r.mu.Unlock()
}

func (r *Run) BotOutputs(botID string) (map[string]any, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.outputs[botID]
	return v, ok
}

// AllOutputs returns every bot's outputs recorded so far, keyed by bot
// instance id — what the WebUI shows once a run finishes, so "it succeeded"
// comes with an actual answer to "so what did it produce."
func (r *Run) AllOutputs() map[string]map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make(map[string]map[string]any, len(r.outputs))
	for k, v := range r.outputs {
		out[k] = v
	}
	return out
}

// RequestApproval registers a pending approval and blocks until Decide is
// called for it (from the WebUI) or timeout elapses.
// onOpen, when given, is called with the new approval's id once it is
// queued and before the wait begins. It exists so a caller can mirror the
// same question somewhere else — 1Claw's mobile queue — and answer this one
// with Decide when that somewhere else replies first. Variadic so the
// callers that don't (the foundry's review gate) are unchanged, and so this
// package stays ignorant of 1Claw.
func (r *Run) RequestApproval(bot, step, summary, riskTier string, timeout time.Duration, onOpen ...func(approvalID string)) (approved bool, decidedBy string, err error) {
	return r.requestApproval(bot, step, summary, riskTier, nil, timeout, onOpen...)
}

// RequestApprovalWithWrites is RequestApproval plus the bot's declared
// writes, so the prompt can say what approving will let it touch. Separate
// rather than another positional parameter: the foundry's review gate has
// no writes to declare and shouldn't have to pass nil.
func (r *Run) RequestApprovalWithWrites(bot, step, summary, riskTier string, writes []string, timeout time.Duration, onOpen ...func(approvalID string)) (approved bool, decidedBy string, err error) {
	return r.requestApproval(bot, step, summary, riskTier, writes, timeout, onOpen...)
}

func (r *Run) requestApproval(bot, step, summary, riskTier string, writes []string, timeout time.Duration, onOpen ...func(approvalID string)) (approved bool, decidedBy string, err error) {
	pa := &PendingApproval{
		ID: uuid.NewString(), Bot: bot, Step: step, Summary: summary, RiskTier: riskTier,
		Writes: writes, Created: time.Now(), decision: make(chan approvalDecision, 1),
	}
	r.mu.Lock()
	r.approvals[pa.ID] = pa
	r.mu.Unlock()
	r.SetStatus(StatusAwaitingApproval)
	r.Log(bot, step, "awaiting approval (%s): %s", pa.ID, summary)
	for _, f := range onOpen {
		if f != nil {
			f(pa.ID)
		}
	}

	select {
	case d, ok := <-pa.decision:
		if !ok {
			// Closed by SetStatus's terminal sweep: the run ended while a
			// human was still deciding. Not approved, and say why rather
			// than returning a silent false.
			return false, "", fmt.Errorf("the run ended before %q was decided", summary)
		}
		r.mu.Lock()
		delete(r.approvals, pa.ID)
		// Only back to running if the run is still alive. A container can
		// hit its own max_runtime_secs while a human is still deciding —
		// 60s budgets with a person in the loop make that the normal case,
		// not an edge one — and the run is then already failed and written
		// to history. Flipping it back to "running" left a run with
		// finished_at and error both set, status running, and nothing left
		// alive to ever move it again: on-disk history said failed while
		// the API said running, forever.
		alive := r.Status != StatusSucceeded && r.Status != StatusFailed
		r.mu.Unlock()
		if alive {
			r.SetStatus(StatusRunning)
		}
		return d.approved, d.decidedBy, nil
	case <-time.After(timeout):
		r.mu.Lock()
		delete(r.approvals, pa.ID)
		r.mu.Unlock()
		// Names what was being approved, not the approval's UUID. The id is
		// no use to the person reading the Runs page — they cannot look it
		// up, it is gone from the queue by the time they see it, and the
		// one thing they need to recognise is which decision they missed.
		return false, "", fmt.Errorf("nobody answered %q within %s", summary, timeout)
	}
}

// Decide resolves a pending approval — called from the REST API when a human
// approves or rejects in the WebUI.
func (r *Run) Decide(approvalID string, approved bool, decidedBy string) error {
	// One critical section, not a lookup followed by a send: SetStatus's
	// terminal sweep closes these channels, and sending on one it just
	// closed would panic. The send itself never blocks — the channel is
	// buffered with room for exactly this one decision.
	r.mu.Lock()
	pa, ok := r.approvals[approvalID]
	if !ok {
		r.mu.Unlock()
		return fmt.Errorf("no pending approval %s on run %s (it may have timed out, or the run may have ended)", approvalID, r.ID)
	}
	if pa.decided {
		r.mu.Unlock()
		return fmt.Errorf("approval %s on run %s was already answered", approvalID, r.ID)
	}
	pa.decided = true
	if !approved {
		r.DeclinedByUser = true
	}
	pa.decision <- approvalDecision{approved: approved, decidedBy: decidedBy}
	r.mu.Unlock()
	// Outside the lock, same reason as SetStatus's terminal callbacks:
	// pending_approval_count already dropped here, before requestApproval's
	// own goroutine wakes and calls SetStatus(Running) — without this, a
	// decision the app just recorded wouldn't show up until that second,
	// later change.
	r.notifyChanged()
	return nil
}

// WasDeclinedByUser reports whether a human answered "no" to one of this
// run's approvals.
func (r *Run) WasDeclinedByUser() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.DeclinedByUser
}

// releaseApprovals unblocks anything waiting on a human.
//
// Cancelling the context does not reach an approval gate: it is waiting on
// pa.decision and its own timeout, not on the run's context. Without this,
// stopping a run parked at "Approval needed" would cancel the containers
// that are not running and leave the one thing that is — a wait for a
// person — to sit there for the rest of its thirty-minute window.
//
// Closing rather than sending false: the receiver already distinguishes the
// two (see RequestApproval's `if !ok`), and a closed channel says "nobody is
// going to answer this" where a false would say "someone said no".
func (r *Run) releaseApprovals() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, pa := range r.approvals {
		delete(r.approvals, id)
		close(pa.decision)
	}
}

// PendingApprovals is what still needs a human. An approval that has been
// answered is not pending, even in the moment before the waiting goroutine
// removes it from the map — a question you have answered must stop being
// asked immediately, in the CLI prompt loop and in every browser tab
// polling GET /api/runs/{id}.
func (r *Run) PendingApprovals() []*PendingApproval {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*PendingApproval, 0, len(r.approvals))
	for _, pa := range r.approvals {
		if pa.decided {
			continue
		}
		out = append(out, pa)
	}
	return out
}
