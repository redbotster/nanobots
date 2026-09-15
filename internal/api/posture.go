package api

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/redbotster/nanobots/internal/oneclaw"
)

// What 1Claw already knows about this account, folded into the page that
// already reports how the machine is set up.
//
// 1Claw exports OpenTelemetry now: a topology of every agent, the policy it
// holds and the vault that grants, plus a posture score and a live signal
// stream. Every bot this repo runs gets its own agent, so most of that graph
// is nanobots' own doing — and none of it was visible from here.
//
// Deliberately not a new page. The Settings System block already answers
// "is this set up and working", and two of the three numbers here are only
// interesting when something is wrong, which is exactly what that block is
// shaped for.

// PostureResponse is the Settings row's worth of 1Claw state.
type PostureResponse struct {
	// Configured is false when there is no 1Claw key. Everything else is
	// meaningless then, and the UI says "not connected" rather than zero.
	Configured bool `json:"configured"`
	Score      int  `json:"score"`
	Threats    int  `json:"threats"`
	Critical   int  `json:"critical"`
	Pending    int  `json:"pending"`

	// Agents is how many 1Claw agents exist, how many the plan allows, and
	// how many are this repo's. The cap is a real, documented failure mode
	// — EnsureAgent returns 403 "Agent limit reached" mid-run when it is
	// hit — and it is the one number here that lets someone act before
	// that happens rather than after.
	Agents         int    `json:"agents"`
	AgentLimit     int    `json:"agent_limit,omitempty"`
	NanobotsAgents int    `json:"nanobots_agents"`
	AgentsNearCap  bool   `json:"agents_near_cap"`
	Tier           string `json:"tier,omitempty"`

	// Error is 1Claw being unreachable or refusing. Reported rather than
	// swallowed: a posture of zero and a posture we could not read are
	// very different, and only one of them is alarming.
	Error string `json:"error,omitempty"`
}

// nanobotsAgentPrefix is how EnsureAgent names the agents it provisions.
// Counting them separates "your org has 25 agents" from "this app made 24
// of them", which is the difference between a number and a fact you can do
// something about.
const nanobotsAgentPrefix = "nanobots-"

// postureTTL is how long a posture reading is served before being re-read.
//
// Shorter than the connections TTL because two of these numbers move on
// their own: pending approvals appear when a run asks for one, and the
// agent count grows whenever a swarm runs a bot that has never run before.
// Nothing in this app can invalidate on those, so the TTL is the only
// freshness there is — thirty seconds keeps the Settings block honest while
// still collapsing a page's worth of repeat loads into one read.
const postureTTL = 30 * time.Second

func (s *Server) handlePosture(w http.ResponseWriter, r *http.Request) {
	if s.OneClaw == nil || !s.OneClaw.Configured() {
		writeJSONCached(w, r, http.StatusOK, PostureResponse{})
		return
	}
	out, err := s.postureCache.do(postureTTL, func() (PostureResponse, error) {
		return s.readPosture()
	})
	if err != nil {
		// 200 with an error field, not a 5xx: this is one row on a settings
		// page, and a page that fails to render because a status widget
		// could not reach a third party is worse than the widget saying so.
		//
		// The error also told the cache not to remember this — a blip kept
		// for thirty seconds reads as an outage, and "1Claw is down" is the
		// one answer worth asking about again immediately.
		writeJSON(w, http.StatusOK, out)
		return
	}
	writeJSONCached(w, r, http.StatusOK, out)
}

// readPosture assembles one reading. Separate from the handler so the cache
// has a plain function to call and the handler has no branch that can
// forget to populate it.
func (s *Server) readPosture() (PostureResponse, error) {
	out := PostureResponse{Configured: true}

	// Three independent 1Claw reads. In sequence they were 2.8s on every
	// single call, measured against the live account — the same shape as
	// /api/connections, and this one is on the Settings page a user opens
	// to find out why something is not working.
	//
	// The summary decides whether there is a row at all; the other two only
	// add detail, so their errors are dropped rather than reported. They
	// still run concurrently with it, because the common case is that all
	// three succeed and waiting for the summary first would spend a round
	// trip to learn nothing.
	var (
		p        *oneclaw.Posture
		postErr  error
		quota    *oneclaw.Quota
		topology *oneclaw.Topology
		wg       sync.WaitGroup
	)
	wg.Add(3)
	go func() { defer wg.Done(); p, postErr = s.OneClaw.OTelPosture() }()
	go func() {
		defer wg.Done()
		if q, err := s.OneClaw.Quota(); err == nil {
			quota = q
		}
	}()
	go func() {
		defer wg.Done()
		if t, err := s.OneClaw.OTelTopology(); err == nil {
			topology = t
		}
	}()
	wg.Wait()

	if postErr != nil {
		// Returned as both a value and an error: the value is what the
		// Settings row shows, the error is what stops it being cached.
		out.Error = postErr.Error()
		return out, postErr
	}
	out.Score, out.Threats, out.Critical = p.Score, p.OpenThreats, p.OpenCritical
	out.Pending, out.Agents = p.PendingApprovals, p.AgentCount

	// Best-effort from here. A missing quota or topology costs a detail,
	// not the row.
	if quota != nil {
		out.Tier = quota.Tier
		out.AgentLimit = quota.Usage.Agents.Limit
		out.AgentsNearCap = quota.Usage.Agents.Near()
		if quota.Usage.Agents.Used > 0 {
			out.Agents = quota.Usage.Agents.Used
		}
	}
	if topology != nil {
		for _, n := range topology.Nodes {
			if n.Kind == "agent" && strings.HasPrefix(n.Label, nanobotsAgentPrefix) {
				out.NanobotsAgents++
			}
		}
	}

	return out, nil
}
