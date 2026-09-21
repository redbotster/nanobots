package api

import (
	"context"
	"fmt"

	"github.com/redbotster/nanobots/internal/lab"
)

// ComposeAndSaveAutomation is Lab's "automate" action: the same
// compose-and-type-check path as the WebUI's compose box
// (composeSwarm), but saved for real immediately rather than handed back
// for a human to review in the builder first (saveSwarm — the same path
// "Save" in the builder uses, merge-not-replace included).
//
// This is a deliberate, disclosed departure from compose.go's own "never
// saves or runs anything on its own" rule, scoped to exactly this one
// caller — see internal/lab's package doc comment for why writing a swarm
// file this way still isn't the elevated-trust shortcut
// context/TEAM-LAB-DESIGN.md rules out. It never runs the swarm: that is
// still a human pressing Run, same as any other saved swarm.
func (s *Server) ComposeAndSaveAutomation(ctx context.Context, message string) (lab.AutomateResult, error) {
	draft, gap, _, _, err := s.composeSwarm(ctx, message)
	if err != nil {
		return lab.AutomateResult{}, err
	}
	if gap != nil {
		return lab.AutomateResult{Gap: gap.MissingCapability}, nil
	}

	resp, _, err := s.saveSwarm(*draft)
	if err != nil {
		return lab.AutomateResult{}, err
	}
	return lab.AutomateResult{
		Name:        resp.Name,
		Path:        resp.Path,
		Description: resp.Description,
		PlanOK:      resp.Plan.OK,
		PlanError:   describePlanProblem(resp.Plan),
	}, nil
}

// describePlanProblem names the first thing wrong with a plan that isn't
// OK, for a human reading Lab's chat rather than the builder's own
// per-field error display. A swarm-level Error (a bad DAG, an unresolved
// bot ref) is the plainest signal when present; otherwise the most common
// real failure is one snap that doesn't type-check, which planResponse
// carries per-snap rather than on the top-level Error field.
func describePlanProblem(plan planResponse) string {
	if plan.OK {
		return ""
	}
	if plan.Error != "" {
		return plan.Error
	}
	for _, sc := range plan.Snaps {
		if !sc.OK {
			return fmt.Sprintf("%s -> %s: %s", sc.From, sc.To, sc.Error)
		}
	}
	for _, u := range plan.Unfed {
		return fmt.Sprintf("%s.%s is required but nothing fills it: %s", u.Bot, u.Port, u.Reason)
	}
	if len(plan.Invalid) > 0 {
		return plan.Invalid[0]
	}
	return "something doesn't connect yet"
}
