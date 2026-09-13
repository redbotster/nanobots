package planner

import (
	"fmt"
	"strings"

	"github.com/redbotster/nanobots/internal/schema"
)

// PlanResult is everything `nanobots plan` needs to report: the resolved
// swarm, every snap's type-check outcome, and the run DAG (nil if the DAG
// couldn't be built, e.g. a cycle).
type PlanResult struct {
	Resolved *ResolvedSwarm
	Snaps    []SnapCheck
	DAG      *DAG
	DAGErr   error
	// Invalid are swarm-level mistakes that aren't about a snap or a port
	// — today, an unrecognised on_error value.
	Invalid []error
	// Unfed are required input ports with no value source. A plan-time
	// property that used to surface only at run time, several containers
	// in. See CheckRequiredInputs.
	Unfed []UnfedInput
}

// OK reports whether the swarm is runnable: every snap type-checked and the
// DAG has no cycles.
func (p *PlanResult) OK() bool {
	if p.DAGErr != nil || len(p.Unfed) > 0 || len(p.Invalid) > 0 {
		return false
	}
	for _, s := range p.Snaps {
		if !s.OK {
			return false
		}
	}
	return true
}

// Plan loads a Nanoswarm from swarmPath, resolves its bots, type-checks its
// snaps, and builds the run DAG. It returns a PlanResult even on
// type-check/DAG failures (so callers can print a full report) — the
// returned error is only for problems that prevent planning from running at
// all (bad YAML, an unresolvable bot reference).
func Plan(swarmPath, botsDir string) (*PlanResult, error) {
	sw, err := schema.LoadNanoswarm(swarmPath)
	if err != nil {
		return nil, err
	}
	return PlanSwarm(sw, botsDir)
}

// PlanSwarm is Plan without the file load — same resolve/type-check/DAG
// pipeline, for a Nanoswarm already in memory. The WebUI's visual builder
// uses this to type-check a swarm as it's being built, before it's ever
// saved to a YAML file (see internal/api/builder.go).
func PlanSwarm(sw *schema.Nanoswarm, botsDir string) (*PlanResult, error) {
	resolved, err := Resolve(sw, botsDir)
	if err != nil {
		return nil, err
	}
	result := &PlanResult{Resolved: resolved}
	result.Snaps = TypeCheckSnaps(resolved)
	result.Unfed = CheckRequiredInputs(resolved)
	result.Invalid = append(CheckOnError(resolved), CheckSnapAndValueCollision(resolved)...)
	result.Invalid = append(result.Invalid, CheckRetry(resolved)...)
	dag, err := BuildDAG(resolved)
	result.DAG = dag
	result.DAGErr = err
	return result, nil
}

// Report renders a human-readable plan report: the DAG (or its error), then
// every snap's type-check outcome.
func (p *PlanResult) Report() string {
	var b strings.Builder
	fmt.Fprintf(&b, "swarm: %s\n\n", p.Resolved.Swarm.Metadata.Name)

	b.WriteString("run order:\n")
	if p.DAGErr != nil {
		fmt.Fprintf(&b, "  ERROR: %v\n", p.DAGErr)
	} else {
		for _, line := range strings.Split(strings.TrimRight(p.DAG.Print(), "\n"), "\n") {
			fmt.Fprintf(&b, "  %s\n", line)
		}
	}

	for _, e := range p.Invalid {
		fmt.Fprintf(&b, "\nFAIL %v\n", e)
	}

	if len(p.Unfed) > 0 {
		b.WriteString("\ninputs with nothing to fill them:\n")
		for _, u := range p.Unfed {
			fmt.Fprintf(&b, "  FAIL %v\n", u)
		}
	}

	b.WriteString("\nsnaps:\n")
	if len(p.Snaps) == 0 {
		b.WriteString("  (none)\n")
	}
	for _, s := range p.Snaps {
		if s.OK {
			if s.Joined() {
				// Say what the join did, not just what came out of it.
				fmt.Fprintf(&b, "  OK   %s (%s) --join:%s--> %s (%s)\n",
					s.Snap.From, s.RawFromType, s.Snap.Join, s.Snap.To, s.ToType)
			} else {
				fmt.Fprintf(&b, "  OK   %s (%s) -> %s (%s)\n", s.Snap.From, s.FromType, s.Snap.To, s.ToType)
			}
		} else {
			fmt.Fprintf(&b, "  FAIL %s -> %s: %v\n", s.Snap.From, s.Snap.To, s.Err)
		}
	}

	if p.OK() {
		b.WriteString("\nplan OK — every snap type-checks and the run graph has no cycles.\n")
	} else {
		b.WriteString("\nplan FAILED — fix the ports, not the planner.\n")
	}
	return b.String()
}
