package planner

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func dagOf(nodes []string, edges map[string][]string) *DAG {
	return &DAG{Nodes: nodes, Edges: edges}
}

// Levels groups a DAG into stages where every bot in a stage has all its
// dependencies satisfied by an earlier one — used to be the runner's whole
// scheduling mechanism, and is still the way `nanobots plan` and this page
// answer "does this swarm branch at all" (docs/parallelism.md). The
// grouping has to be exactly right regardless of who reads it: a bot
// placed one stage too early would misdescribe a swarm as safe to
// parallelize a step sooner than its real dependency allows.
func TestLevelsGroupIndependentBotsTogether(t *testing.T) {
	for _, tc := range []struct {
		name  string
		nodes []string
		edges map[string][]string
		want  [][]string
	}{
		{
			// Eleven of the sixteen catalog swarms look like this, and gain
			// nothing from waves — which is worth stating rather than
			// implying the change speeds everything up.
			name:  "a straight chain is one bot per wave",
			nodes: []string{"a", "b", "c"},
			edges: map[string][]string{"a": {"b"}, "b": {"c"}},
			want:  [][]string{{"a"}, {"b"}, {"c"}},
		},
		{
			// morning-brief's shape: two sources feeding two sinks.
			name:  "two independent chains run side by side",
			nodes: []string{"a", "b", "c", "d"},
			edges: map[string][]string{"a": {"c"}, "b": {"d"}},
			want:  [][]string{{"a", "b"}, {"c", "d"}},
		},
		{
			name:  "a diamond fans out then joins",
			nodes: []string{"top", "left", "right", "bottom"},
			edges: map[string][]string{"top": {"left", "right"}, "left": {"bottom"}, "right": {"bottom"}},
			want:  [][]string{{"top"}, {"left", "right"}, {"bottom"}},
		},
		{
			// The join is what matters here: `late` depends on `early`, so
			// it must not be hoisted into the first wave alongside `solo`
			// just because `solo` has no dependencies.
			name:  "a bot waits for its dependency even when others are ready",
			nodes: []string{"early", "late", "solo"},
			edges: map[string][]string{"early": {"late"}},
			want:  [][]string{{"early", "solo"}, {"late"}},
		},
		{
			name:  "no edges at all is one wide wave",
			nodes: []string{"c", "a", "b"},
			edges: map[string][]string{},
			want:  [][]string{{"a", "b", "c"}},
		},
		{
			name:  "one bot",
			nodes: []string{"only"},
			edges: map[string][]string{},
			want:  [][]string{{"only"}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := dagOf(tc.nodes, tc.edges).Levels()
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Levels() = %v, want %v", got, tc.want)
			}
		})
	}
}

// Levels and TopoSort are two readings of the same graph. The runner no
// longer schedules off either — it waits on each bot's own dependency
// edges directly (internal/runner.runDAG) — but `nanobots plan`'s report
// prints both, and they have to agree: if they ever disagreed, a swarm's
// own plan output would show a run order its Levels grouping contradicts.
//
// Checked against every swarm in the repo rather than a fixture, so a new
// swarm shape is covered the day someone adds it.
func TestLevelsFlattenToAValidRunOrderForEveryCatalogSwarm(t *testing.T) {
	root := repoRootForLevels(t)
	swarms, err := filepath.Glob(filepath.Join(root, "examples", "swarms", "*.yaml"))
	if err != nil || len(swarms) == 0 {
		t.Fatalf("no swarms found: %v", err)
	}

	for _, path := range swarms {
		t.Run(filepath.Base(path), func(t *testing.T) {
			result, err := Plan(path, filepath.Join(root, "bots"))
			if err != nil || result.DAG == nil {
				t.Skipf("swarm does not plan: %v", err)
			}
			levels, err := result.DAG.Levels()
			if err != nil {
				t.Fatal(err)
			}

			// Every bot appears exactly once...
			seen := map[string]bool{}
			var flat []string
			for _, wave := range levels {
				for _, b := range wave {
					if seen[b] {
						t.Errorf("%s appears in more than one wave", b)
					}
					seen[b] = true
					flat = append(flat, b)
				}
			}
			if len(flat) != len(result.DAG.Nodes) {
				t.Fatalf("levels hold %d bots, DAG has %d", len(flat), len(result.DAG.Nodes))
			}

			// ...and no bot runs before something it depends on.
			position := map[string]int{}
			for i, b := range flat {
				position[b] = i
			}
			for from, tos := range result.DAG.Edges {
				for _, to := range tos {
					if position[from] >= position[to] {
						t.Errorf("%s runs at or after %s, but %s feeds it", from, to, from)
					}
				}
			}

			// The stronger property the runner actually relies on: within
			// one wave there is no edge at all, so nothing in a wave can
			// read anything else in it.
			waveOf := map[string]int{}
			for i, wave := range levels {
				for _, b := range wave {
					waveOf[b] = i
				}
			}
			for from, tos := range result.DAG.Edges {
				for _, to := range tos {
					if waveOf[from] == waveOf[to] {
						t.Errorf("%s and %s are in the same wave but %s feeds %s —"+
							" running them together would read an output that doesn't exist yet",
							from, to, from, to)
					}
				}
			}
		})
	}
}

// A cycle has to be reported the same way whichever entry point finds it,
// or the same broken swarm produces two different explanations.
func TestLevelsReportsACycleTheSameWayTopoSortDoes(t *testing.T) {
	d := dagOf([]string{"a", "b"}, map[string][]string{"a": {"b"}, "b": {"a"}})

	_, levelsErr := d.Levels()
	_, topoErr := d.TopoSort()
	if levelsErr == nil {
		t.Fatal("Levels accepted a cycle")
	}
	if topoErr == nil {
		t.Fatal("TopoSort accepted a cycle")
	}
	if levelsErr.Error() != topoErr.Error() {
		t.Errorf("different messages for the same cycle:\n  Levels:   %v\n  TopoSort: %v", levelsErr, topoErr)
	}
	if !strings.Contains(levelsErr.Error(), "a") || !strings.Contains(levelsErr.Error(), "b") {
		t.Errorf("the message does not name the bots in the cycle: %v", levelsErr)
	}
}

func repoRootForLevels(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Join(filepath.Dir(file), "..", "..")
	if _, err := os.Stat(filepath.Join(root, "bots")); err != nil {
		t.Fatalf("wrong repo root %s: %v", root, err)
	}
	return root
}
