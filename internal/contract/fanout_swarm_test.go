package contract

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/redbotster/nanobots/internal/planner"
	"github.com/redbotster/nanobots/internal/schema"
)

// Fanning one bot out over two source lists iterates them in lockstep, so
// they have to be the same length. Nothing checked that until the run was
// already half done — support-desk-lite fanned `sender` over
// `triage.draft_ids.*` and `triage.tickets.*.subject`, which are different
// lengths by construction (an escalated ticket gets no draft), and died
// mid-swarm after the triage bot had already called Gmail.
//
// It survived because it only shows up at runtime, with real list lengths,
// and because it was hidden behind an unrelated failure in the same wave.
//
// This catches the class before anything runs, using each source bot's own
// conformance outputs as the example data. That is a proxy, not a proof —
// real lists could still diverge where the fixtures happen to agree — but
// a bot whose own demo data disagrees is broken for the one input set it
// ships, which is a low bar to clear and this swarm did not clear it.
func TestNoSwarmFansOutOverListsOfDifferentLengths(t *testing.T) {
	root := repoRoot(t)
	swarms, err := filepath.Glob(filepath.Join(root, "examples", "swarms", "*.yaml"))
	if err != nil || len(swarms) == 0 {
		t.Fatalf("no swarms found: %v", err)
	}

	// One conformance run per bot, reused across every swarm that uses it.
	outputs := map[string]map[string]any{}
	outputsFor := func(botID string) map[string]any {
		if o, ok := outputs[botID]; ok {
			return o
		}
		report, err := RunConformance(filepath.Join(root, "bots", botID), "")
		if err != nil || report == nil {
			outputs[botID] = nil
			return nil
		}
		outputs[botID] = report.Outputs
		return report.Outputs
	}

	checked := 0
	for _, path := range swarms {
		name := filepath.Base(path)
		loaded, err := schema.LoadNanoswarm(path)
		if err != nil {
			// Malformed swarms are another test's problem.
			continue
		}
		sw := *loaded

		for _, b := range sw.Spec.Bots {
			fo, err := planner.FanOutFor(&sw, b.ID)
			if err != nil || fo == nil || len(fo.Snaps) < 2 {
				continue
			}
			checked++

			lengths := map[string]int{}
			for _, snap := range fo.Snaps {
				ep, err := planner.ParseEndpoint(snap.From)
				if err != nil {
					continue
				}
				src := botUseID(&sw, ep.BotID)
				if src == "" {
					continue
				}
				out := outputsFor(src)
				if out == nil {
					continue
				}
				list, ok := out[ep.Port].([]any)
				if !ok {
					continue
				}
				lengths[snap.From] = len(list)
			}
			if len(lengths) < 2 {
				continue
			}

			var first string
			for ref := range lengths {
				if first == "" || ref < first {
					first = ref
				}
			}
			for ref, n := range lengths {
				if n == lengths[first] {
					continue
				}
				t.Errorf("%s: bot %q fans out over lists of different lengths in the source bots' own"+
					" demo data — %s has %d item(s), %s has %d.\n"+
					"They iterate together on one index, so this fails mid-run. Snap both sides from"+
					" one list whose items carry everything the target needs.",
					name, b.ID, ref, n, first, lengths[first])
			}
		}
	}

	if checked == 0 {
		t.Fatal("no swarm fans a bot out over more than one list — this test is checking nothing")
	}
}

// botUseID maps a swarm-local bot id ("triage") to the catalog directory it
// uses ("support-triage"), dropping any @version suffix.
func botUseID(sw *schema.Nanoswarm, localID string) string {
	for _, b := range sw.Spec.Bots {
		if b.ID != localID {
			continue
		}
		use := b.Use
		if i := strings.Index(use, "@"); i >= 0 {
			use = use[:i]
		}
		return use
	}
	return ""
}
