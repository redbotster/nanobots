package runner

import (
	"fmt"
	"sync"
	"time"
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
// TODO(nanobots#approvals-mirror): also mirror live bots' approvals into
// 1Claw's own approval queue (dashboard/mobile), not just the local WebUI.
type RunQueueApprover struct {
	Run  *Run
	Bot  string
	Step string
}

func (a *RunQueueApprover) Approve(summary, riskTier string) (bool, string, error) {
	return a.Run.RequestApproval(a.Bot, a.Step, summary, riskTier, approvalTimeout)
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
