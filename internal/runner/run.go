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

	mu          sync.Mutex
	log         []LogEntry
	outputs     map[string]map[string]any // botID -> port -> value (JSON-safe)
	approvals   map[string]*PendingApproval
	subscribers map[chan LogEntry]bool
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
	r.log = append(r.log, entry)
	subs := make([]chan LogEntry, 0, len(r.subscribers))
	for ch := range r.subscribers {
		subs = append(subs, ch)
	}
	r.mu.Unlock()
	for _, ch := range subs {
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
	r.mu.Lock()
	delete(r.subscribers, ch)
	r.mu.Unlock()
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
	if s == StatusSucceeded || s == StatusFailed {
		r.FinishedAt = time.Now()
	}
	r.mu.Unlock()
}

func (r *Run) SetError(err error) {
	r.mu.Lock()
	r.Error = err.Error()
	r.mu.Unlock()
}

// GetStatus/GetError/GetFinishedAt are synchronized reads of the fields
// SetStatus/SetError mutate under r.mu — found necessary (not
// hypothetical: caught by go test -race) once a second consumer besides
// this run's own goroutine started polling a live run's JSON shape
// (internal/foundry's Job embeds *Run and is polled the same way an
// in-progress swarm run already was via runToJSON). ID/SwarmName/StartedAt
// are write-once at NewRun and never mutated again, so reading them
// directly elsewhere isn't a race the same way.
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
	case d := <-pa.decision:
		r.mu.Lock()
		delete(r.approvals, pa.ID)
		r.mu.Unlock()
		r.SetStatus(StatusRunning)
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
	r.mu.Lock()
	pa, ok := r.approvals[approvalID]
	r.mu.Unlock()
	if !ok {
		return fmt.Errorf("no pending approval %s on run %s", approvalID, r.ID)
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
