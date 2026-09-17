package runner

import (
	"fmt"
	"sync"

	"github.com/redbotster/nanobots/internal/agentname"
	"github.com/redbotster/nanobots/internal/oneclaw"
)

// ApprovalAgentName is the one agent nanobots asks with.
//
// Opening a 1Claw approval requires an agent — a human key is refused 403
// — and the obvious implementation, an agent per approving bot, is the
// wrong one twice over. Agents are plan-capped (this account sits at 27 of
// 50, every one of them created by this repo), and a question addressed to
// you does not become clearer for being asked by `nanobots-email-send-approved`
// rather than by nanobots.
//
// So: one agent, shared by every approval, created on the first gate that
// opens and never again. It is also the name `nanobots deploy 1claw`
// expects to run as, and the direction Phase 2 item 15 takes the rest of
// the per-bot agents in.
const ApprovalAgentName = agentname.Approvals

// approvalAgent resolves the shared approvals agent, creating it on first
// use, and returns a client authenticated *as that agent*.
//
// Resolved lazily rather than at startup: a deployment that never opens an
// approval should never create the agent, and a machine with no 1Claw at
// all must not pay a network round trip to discover that.
//
// The client is cached for the life of the orchestrator. EnsureAgent is
// cheap on the happy path — it checks a cached listing against a saved
// credential — but "cheap" is still a call, and every fanned-out item in a
// twenty-invoice batch would make one.
func (o *Orchestrator) approvalAgent() (*oneclaw.Client, string, error) {
	if o.OneClaw == nil || !o.OneClaw.Configured() {
		return nil, "", nil
	}

	o.approvalAgentMu.Lock()
	defer o.approvalAgentMu.Unlock()
	if o.approvalAgentClient != nil || o.approvalAgentErr != nil {
		return o.approvalAgentClient, o.approvalAgentID, o.approvalAgentErr
	}

	id, apiKey, err := o.OneClaw.EnsureAgent(o.AgentStateDir, ApprovalAgentName, oneclaw.CreateAgentRequest{
		Name: ApprovalAgentName,
		// Nothing else. This agent exists to ask a question and read the
		// answer: no Shroud budget to spend, no memory to hold, no vault to
		// reach, no execution intents to run. An agent that can do less is
		// the right one to hand a credential file on a laptop.
		SystemPrompt: "Asks the human to approve an action a nanobot is about to take.",
	})
	if err != nil {
		// Cached, deliberately. The common failure is "an agent named
		// nanobots exists on 1Claw but its key was shown once and we do not
		// have it", which no amount of retrying fixes — and retrying it per
		// approval would put one API call per gate behind a wall of
		// identical log lines.
		o.approvalAgentErr = fmt.Errorf("1Claw approvals agent: %w", err)
		return nil, "", o.approvalAgentErr
	}

	o.approvalAgentClient = oneclaw.NewAgentClient(apiKey)
	o.approvalAgentID = id
	return o.approvalAgentClient, o.approvalAgentID, nil
}

// approvalAgentState is embedded in Orchestrator; kept here so the whole
// concept is one file.
type approvalAgentState struct {
	approvalAgentMu     sync.Mutex
	approvalAgentClient *oneclaw.Client
	approvalAgentID     string
	approvalAgentErr    error
}
