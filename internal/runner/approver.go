package runner

import "time"

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
