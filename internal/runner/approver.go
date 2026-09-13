package runner

import (
	"fmt"
	"sync"
	"time"

	"github.com/redbotster/nanobots/internal/oneclaw"
)

// approvalTimeout bounds how long a run waits on a human decision before
// failing the step outright — long enough for a person to notice a mobile
// push or come back to their laptop, short enough that a run doesn't hang
// forever if nobody's watching.
const approvalTimeout = 30 * time.Minute

// RunQueueApprover routes an `approve` step to a Run's own pending-approval
// queue (surfaced over SSE and decided via the REST API), rather than
// DemoDeps' instant auto-approve or LiveDeps' direct 1Claw round-trip. Both
// Deps implementations accept this as an override so approvals behave
// identically whether the bot behind them is running against real 1Claw or
// demo fixtures — see run.go's PendingApproval doc comment.
//
// When 1Claw is configured, the same question is also opened in its own
// approval queue, so an overnight swarm can be answered from a phone
// instead of only from a browser tab that happens to be open. Whichever
// answers first wins; the other is ignored.
type RunQueueApprover struct {
	Run  *Run
	Bot  string
	Step string
	// Writes is the bot's guardrails.writes_allowed, shown in the prompt so
	// the person answering can see what "yes" permits.
	Writes []string

	// OneClaw and the agent mirror the approval into 1Claw's queue. No
	// client or no agent means local-only, which is what a demo run and a
	// deployment without 1Claw both get.
	OneClaw *oneclaw.Client
	AgentID string
	// AgentIDFn resolves the agent when the gate opens rather than when the
	// approver is built — the fan-out case builds its approver before the
	// first item has run. Takes precedence over AgentID.
	AgentIDFn func() string
}

// mirrorPoll is how often the 1Claw queue is checked. Slower than a local
// decision, which is instant — this is a network round trip against a
// question a human is thinking about, not a hot loop.
var mirrorPoll = 5 * time.Second

func (a *RunQueueApprover) Approve(summary, riskTier string) (bool, string, error) {
	// Closed when the local wait returns, so the mirror's poll loop stops
	// rather than running for the full thirty-minute timeout after the
	// question has already been answered here.
	done := make(chan struct{})
	defer close(done)

	return a.Run.RequestApprovalWithWrites(a.Bot, a.Step, summary, riskTier, a.Writes, approvalTimeout,
		func(localID string) { a.mirror(localID, summary, riskTier, done) })
}

// mirror opens the same approval in 1Claw and, if it is answered there
// first, decides the local one.
//
// Every failure here is logged and dropped rather than propagated: the
// local queue is the one that must work. A 1Claw outage should cost you the
// convenience of approving from your phone, not the ability to approve at
// all.
func (a *RunQueueApprover) mirror(localID, summary, riskTier string, done <-chan struct{}) {
	agentID := a.AgentID
	if a.AgentIDFn != nil {
		agentID = a.AgentIDFn()
	}
	if a.OneClaw == nil || !a.OneClaw.Configured() || agentID == "" {
		return
	}
	ap, err := a.OneClaw.RequestApproval(agentID, summary, riskTier)
	if err != nil {
		a.Run.Log(a.Bot, a.Step, "could not also ask on 1Claw, so this is answerable here only: %v", err)
		return
	}
	a.Run.Log(a.Bot, a.Step, "also asked on 1Claw (%s) — answer it here or on your phone", ap.ID)

	go func() {
		ticker := time.NewTicker(mirrorPoll)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				// Answered here. The 1Claw approval is left pending rather
				// than withdrawn: its API has no cancel, and a stale
				// question is better than pretending to have one.
				return
			case <-ticker.C:
				status, err := a.OneClaw.ApprovalStatus(ap.ID)
				if err != nil || status == "pending" {
					continue
				}
				// Decide fails harmlessly if the local one was already
				// answered in the moment between the poll and this call.
				_ = a.Run.Decide(localID, status == "approved", "1claw ("+status+")")
				return
			}
		}
	}()
}

// BatchApprover asks once for a whole fanned-out batch and reuses the
// answer for every item.
//
// Without it, sending twenty invoice reminders would open twenty gates —
// which in practice means nobody reads them, and the twentieth is approved
// reflexively. One gate that names the count is the honest trade: it is a
// single click authorising twenty real sends, so the summary has to say so.
//
// Wraps a RunQueueApprover rather than replacing it, so a batch approval is
// the same pending approval, in the same queue, answered the same way.
type BatchApprover struct {
	Inner *RunQueueApprover
	Total int

	mu       sync.Mutex
	asked    bool
	approved bool
	by       string
	err      error
}

func (a *BatchApprover) Approve(summary, riskTier string) (bool, string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.asked {
		return a.approved, a.by, a.err
	}
	a.asked = true
	a.approved, a.by, a.err = a.Inner.Approve(batchSummary(summary, a.Total), riskTier)
	return a.approved, a.by, a.err
}

// batchSummary makes the scale of what's being authorised unmissable — the
// whole risk of one-click batch approval is someone reading a summary
// written for a single item.
func batchSummary(summary string, total int) string {
	if total <= 1 {
		return summary
	}
	return fmt.Sprintf("%s — and %d more like it (%d in total, approving covers all of them)",
		summary, total-1, total)
}
