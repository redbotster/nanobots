// Package runner is the local execution target: it turns a planned Nanoswarm
// into real Docker containers, one per bot in topological order, wiring each
// bot's declared inputs from swarm vars, upstream snaps, and defaults, and
// collecting its outputs back onto the context bus for the next bot. See
// NANOBOTS-BLUEPRINT.md §3.1 and §3.4.
package runner

import (
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

type RunStatus string

const (
	StatusPending          RunStatus = "pending"
	StatusRunning          RunStatus = "running"
	StatusAwaitingApproval RunStatus = "awaiting_approval"
	StatusSucceeded        RunStatus = "succeeded"
	StatusFailed           RunStatus = "failed"
)

// LogEntry is one line of a run's aggregated log, across all its bots.
type LogEntry struct {
	Time time.Time `json:"time"`
	Bot  string    `json:"bot"`
	Step string    `json:"step,omitempty"`
	Msg  string    `json:"msg"`
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

	decision chan approvalDecision
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
	// run`) or "schedule" (internal/scheduler fired it) — set once at
	// construction/immediately after and never mutated again, so reading
	// it directly elsewhere carries the same no-further-writes safety as
	// ID/SwarmName (see GetStatus's own doc comment for the fields that
	// *do* need synchronized access). Exists so the Runs page can show a
	// run nobody clicked "why did this happen" instead of a mystery entry.
	TriggeredBy string `json:"triggered_by"`
	// SwarmPath is the file this run was planned from — the one fact needed
	// to run the same thing again. Set once by ExecuteSwarm and never
	// mutated (same safety argument as TriggeredBy). Empty for a foundry
	// job, which embeds a Run but was never planned from a swarm file.
	SwarmPath string `json:"swarm_path,omitempty"`
	// Tolerated are bots that failed while the swarm was told to continue
	// without them. Guarded by mu — read it with GetTolerated.
	Tolerated []ToleratedFailure `json:"tolerated,omitempty"`

	mu            sync.Mutex
	log           []LogEntry
	outputs       map[string]map[string]any // botID -> port -> value (JSON-safe)
	approvals     map[string]*PendingApproval
	subscribers   map[chan LogEntry]bool
	onTerminalFns []func(*Run)
}

func NewRun(swarmName string) *Run {
	return &Run{
		ID:          uuid.NewString(),
		SwarmName:   swarmName,
		TriggeredBy: "manual",
		Status:      StatusPending,
		StartedAt:   time.Now(),
		outputs:     map[string]map[string]any{},
		approvals:   map[string]*PendingApproval{},
		subscribers: map[chan LogEntry]bool{},
	}
}

func (r *Run) Log(bot, step, format string, a ...any) {
	entry := LogEntry{Time: time.Now(), Bot: bot, Step: step, Msg: fmt.Sprintf(format, a...)}
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

// AddTolerated records a failure the swarm chose to continue past.
func (r *Run) AddTolerated(bot, msg string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Tolerated = append(r.Tolerated, ToleratedFailure{Bot: bot, Error: msg})
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
func (r *Run) RequestApproval(bot, step, summary, riskTier string, timeout time.Duration) (approved bool, decidedBy string, err error) {
	pa := &PendingApproval{
		ID: uuid.NewString(), Bot: bot, Step: step, Summary: summary, RiskTier: riskTier,
		Created: time.Now(), decision: make(chan approvalDecision, 1),
	}
	r.mu.Lock()
	r.approvals[pa.ID] = pa
	r.mu.Unlock()
	r.SetStatus(StatusAwaitingApproval)
	r.Log(bot, step, "awaiting approval (%s): %s", pa.ID, summary)

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
		return false, "", fmt.Errorf("approval %s timed out after %s", pa.ID, timeout)
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
	defer r.mu.Unlock()
	pa, ok := r.approvals[approvalID]
	if !ok {
		return fmt.Errorf("no pending approval %s on run %s (it may have timed out, or the run may have ended)", approvalID, r.ID)
	}
	pa.decision <- approvalDecision{approved: approved, decidedBy: decidedBy}
	return nil
}

func (r *Run) PendingApprovals() []*PendingApproval {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*PendingApproval, 0, len(r.approvals))
	for _, pa := range r.approvals {
		out = append(out, pa)
	}
	return out
}
