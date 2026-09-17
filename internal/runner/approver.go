package runner

import (
	"fmt"
	"sync"
	"time"

	"github.com/redbotster/nanobots/internal/oneclaw"
)

// ApprovalTimeout bounds how long a run waits on a human decision before
// failing the step outright — long enough for a person to notice a mobile
// push or come back to their laptop, short enough that a run doesn't hang
// forever if nobody's watching.
//
// In practice it almost never fires, and it is worth knowing why before
// tuning it. The container running the bot has its own budget, the bot's
// `max_runtime_secs`, and that is the shorter of the two for every bot in
// the catalog: most are 30-240 seconds, and the three that request
// approvals use 1800 — exactly this value, so they tie and the container's
// SIGKILL lands first. Either way the container dies before this timeout
// gets to say anything, which is how all 54 approval waits on this machine
// came to be reported as "container exceeded 30m0s and was stopped".
//
// describeTimeout in execute.go is what makes that case honest, by naming
// the unanswered approval when a timeout coincides with one. Raising a
// bot's max_runtime_secs past this value is what would make this timeout
// fire instead, and let the bot exit cleanly rather than being killed —
// deliberately not done here, because a bot's runtime budget is the bot's
// own declaration and changing it silently for every approving bot is a
// bigger decision than a comment should make.
// Exported because it is a budget other things have to be checked against:
// internal/contract asserts that no bot ships an `approve` step with a
// max_runtime_secs shorter than this. post-publisher shipped with 60, which
// gave a person one minute to answer "Publish this post to X and LinkedIn?"
// and killed four real runs in two days.
const ApprovalTimeout = 30 * time.Minute

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

	// Mirror resolves the agent that opens the 1Claw copy of this approval.
	// nil, or a nil client, means local-only, which is what a run without
	// 1Claw gets.
	Mirror ApprovalMirror
}

// ApprovalMirror resolves the agent a 1Claw approval is opened by.
//
// Called when the gate opens rather than when the approver is built: the
// fan-out case builds its approver before the first item has run, and a
// deployment that never opens an approval should never create the agent.
//
// It returns an *agent-authenticated* client, not the human one. That is
// the whole of this feature — see RunQueueApprover.mirror. The
// implementation is Orchestrator.approvalAgent.
type ApprovalMirror func() (client *oneclaw.Client, agentID string, err error)

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

	return a.Run.RequestApprovalWithWrites(a.Bot, a.Step, summary, riskTier, a.Writes, ApprovalTimeout,
		func(localID string) { a.mirror(localID, summary, riskTier, done) })
}

// mirror opens the same approval in 1Claw and, if it is answered there
// first, decides the local one.
//
// Every failure here is logged and dropped rather than propagated: the
// local queue is the one that must work. A 1Claw outage should cost you the
// convenience of approving from your phone, not the ability to approve at
// all.
//
// This was dormant for the life of the repo, and the reason it was dormant
// was a wrong conclusion written down as fact.
//
// `POST /v1/approvals/request` is agent-only. Sent with the human key it
// answers 403 "Only agents can request approvals." An earlier investigation
// then tried the agent's `ocv_` key as a bearer token (401) and tried
// exchanging it at the *human* endpoint `/v1/auth/api-key-token` (401), and
// concluded from those two refusals that an agent could not open an
// approval at all. The step it missed is that an agent key has its own
// exchange:
//
//	POST /v1/auth/agent-token   {"api_key":"ocv_…"}  -> 200, a JWT
//	POST /v1/approvals/request  Authorization: Bearer <JWT>  -> the approval
//	GET  /v1/approvals/{id}/status  same bearer  -> pending|approved|…
//
// All three verified against the live account. So the body this code was
// already building was correct; it was being sent by the wrong client.
//
// What it cost: on this machine, 54 runs died on an approval nobody was
// awake to answer — 27 hours of container time — and across 108 runs that
// opened a gate, not one logged either line below. Seven of the sixteen
// catalog swarms pause for a human.
//
// Listing an agent's own approvals is still human-only (403), which is why
// this polls by id and records nothing it cannot reach again
// (docs/1claw-feature-requests.md #10).
func (a *RunQueueApprover) mirror(localID, summary, riskTier string, done <-chan struct{}) {
	if a.Mirror == nil {
		return
	}
	client, agentID, err := a.Mirror()
	if err != nil {
		a.Run.Log(a.Bot, a.Step, "could not also ask on 1Claw, so this is answerable here only: %v", err)
		return
	}
	if client == nil || !client.Configured() || agentID == "" {
		return
	}
	ap, err := client.RequestApproval(agentID, summary, riskTier)
	if err != nil {
		a.Run.Log(a.Bot, a.Step, "could not also ask on 1Claw, so this is answerable here only: %v", err)
		return
	}
	a.Run.Log(a.Bot, a.Step, "also asked on 1Claw (%s) — answer it here or there", ap.ID)

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
				status, err := client.ApprovalStatus(ap.ID)
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
