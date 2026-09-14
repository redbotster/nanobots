package api

import (
	"net/http"
	"strings"
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

func (s *Server) handlePosture(w http.ResponseWriter, r *http.Request) {
	if s.OneClaw == nil || !s.OneClaw.Configured() {
		writeJSON(w, http.StatusOK, PostureResponse{})
		return
	}

	out := PostureResponse{Configured: true}

	p, err := s.OneClaw.OTelPosture()
	if err != nil {
		// 200 with an error field, not a 5xx: this is one row on a settings
		// page, and a page that fails to render because a status widget
		// could not reach a third party is worse than the widget saying so.
		out.Error = err.Error()
		writeJSON(w, http.StatusOK, out)
		return
	}
	out.Score, out.Threats, out.Critical = p.Score, p.OpenThreats, p.OpenCritical
	out.Pending, out.Agents = p.PendingApprovals, p.AgentCount

	// Best-effort from here. A missing quota or topology costs a detail,
	// not the row.
	if q, err := s.OneClaw.Quota(); err == nil {
		out.Tier = q.Tier
		out.AgentLimit = q.Usage.Agents.Limit
		out.AgentsNearCap = q.Usage.Agents.Near()
		if q.Usage.Agents.Used > 0 {
			out.Agents = q.Usage.Agents.Used
		}
	}
	if t, err := s.OneClaw.OTelTopology(); err == nil {
		for _, n := range t.Nodes {
			if n.Kind == "agent" && strings.HasPrefix(n.Label, nanobotsAgentPrefix) {
				out.NanobotsAgents++
			}
		}
	}
	writeJSON(w, http.StatusOK, out)
}
