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
}

// OK reports whether the swarm is runnable: every snap type-checked and the
// DAG has no cycles.
func (p *PlanResult) OK() bool {
	if p.DAGErr != nil {
		return false
	}
	for _, s := range p.Snaps {
		if !s.OK {
			return false
		}
	}
	return true
}

// Plan loads a Nanoswarm, resolves its bots, type-checks its snaps, and
// builds the run DAG. It returns a PlanResult even on type-check/DAG
// failures (so callers can print a full report) — the returned error is only
// for problems that prevent planning from running at all (bad YAML, an
// unresolvable bot reference).
func Plan(swarmPath, botsDir string) (*PlanResult, error) {
	sw, err := schema.LoadNanoswarm(swarmPath)
	if err != nil {
		return nil, err
	}
	resolved, err := Resolve(sw, botsDir)
	if err != nil {
		return nil, err
	}
	result := &PlanResult{Resolved: resolved}
	result.Snaps = TypeCheckSnaps(resolved)
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

	b.WriteString("\nsnaps:\n")
	if len(p.Snaps) == 0 {
		b.WriteString("  (none)\n")
	}
	for _, s := range p.Snaps {
		if s.OK {
			fmt.Fprintf(&b, "  OK   %s (%s) -> %s (%s)\n", s.Snap.From, s.FromType, s.Snap.To, s.ToType)
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
