package planner

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/redbotster/nanobots/internal/schema"
)

func listOf(base schema.PortType) schema.ParsedType {
	inner := schema.ParsedType{Base: base}
	return schema.ParsedType{Base: "list", List: &inner}
}

// The type a join produces and the value it produces have to agree. They
// live in one file for that reason, and this checks both halves against the
// same inputs — a join the planner accepts must be one the runner can
// perform, or a swarm type-checks and then dies mid-run.
func TestJoinTypeAndValueAgree(t *testing.T) {
	for _, tc := range []struct {
		mode      JoinMode
		from      schema.ParsedType
		wantType  string
		value     any
		wantValue any
	}{
		{
			JoinLines, listOf(schema.PortString), "string",
			[]any{"sent to a@x", "sent to b@y"},
			"sent to a@x\nsent to b@y",
		},
		{
			// Objects render as compact JSON, not Go's %v — a map printed
			// with %v is map[a:1], which is not something to put in
			// someone's Slack channel.
			JoinLines, listOf(schema.PortJSON), "string",
			[]any{map[string]any{"to": "a@x"}, map[string]any{"to": "b@y"}},
			"{\"to\":\"a@x\"}\n{\"to\":\"b@y\"}",
		},
		{
			JoinJSON, listOf(schema.PortString), "string",
			[]any{"one", "two"},
			`["one","two"]`,
		},
		{
			JoinCount, listOf(schema.PortString), "string",
			[]any{"a", "b", "c"},
			"3",
		},
		{
			JoinFirst, listOf(schema.PortString), "string",
			[]any{"first", "second"},
			"first",
		},
		{
			JoinFlatten,
			schema.ParsedType{Base: "list", List: &schema.ParsedType{
				Base: "list", List: &schema.ParsedType{Base: schema.PortJSON}}},
			"list<json>",
			[]any{[]any{"a", "b"}, []any{"c"}},
			[]any{"a", "b", "c"},
		},
	} {
		t.Run(string(tc.mode)+"/"+tc.from.String(), func(t *testing.T) {
			gotType, err := JoinType(tc.mode, tc.from)
			if err != nil {
				t.Fatalf("JoinType: %v", err)
			}
			if gotType.String() != tc.wantType {
				t.Errorf("JoinType = %s, want %s", gotType, tc.wantType)
			}
			gotVal, err := JoinValue(tc.mode, tc.value)
			if err != nil {
				t.Fatalf("JoinValue: %v", err)
			}
			if !reflect.DeepEqual(gotVal, tc.wantValue) {
				t.Errorf("JoinValue = %#v, want %#v", gotVal, tc.wantValue)
			}
		})
	}
}

// A join on something that isn't a list means the author expected a
// fan-out that isn't happening. Catching it at plan time is the whole point
// of type-checking the swarm before running it.
func TestJoiningANonListIsRejectedAtPlanTime(t *testing.T) {
	_, err := JoinType(JoinLines, schema.ParsedType{Base: schema.PortString})
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "list") {
		t.Errorf("err = %v, want it to say a list is needed", err)
	}
}

// flatten is the one mode with a stronger requirement than "a list".
func TestFlattenNeedsAListOfLists(t *testing.T) {
	_, err := JoinType(JoinFlatten, listOf(schema.PortString))
	if err == nil {
		t.Fatal("flatten accepted list<string>")
	}
	if !strings.Contains(err.Error(), "list<list<") {
		t.Errorf("err = %v, want it to name the shape it needs", err)
	}
	// And at run time, an item that isn't a list is reported rather than
	// silently dropped.
	if _, err := JoinValue(JoinFlatten, []any{[]any{"a"}, "not a list"}); err == nil {
		t.Error("flatten accepted a non-list item")
	}
}

// A typo must name the real modes rather than failing obscurely, and the
// list must come from what is implemented.
func TestAnUnknownJoinNamesTheRealOnes(t *testing.T) {
	_, err := JoinType(JoinMode("concat"), listOf(schema.PortString))
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, mode := range JoinModes() {
		if !strings.Contains(err.Error(), mode) {
			t.Errorf("err = %v, does not offer %q", err, mode)
		}
	}
}

// `first` on an empty list has no answer, and inventing one (nil, "") would
// hand a downstream bot a value it would treat as real. This is the case
// `.0` used to fail on too, and it should keep failing.
func TestFirstOnAnEmptyListFails(t *testing.T) {
	if _, err := JoinValue(JoinFirst, []any{}); err == nil {
		t.Error("first on an empty list returned something")
	}
	// The aggregate modes are fine with empty — nothing sent is a real,
	// reportable outcome.
	for _, mode := range []JoinMode{JoinLines, JoinJSON, JoinCount, JoinFlatten} {
		if _, err := JoinValue(mode, []any{}); err != nil {
			t.Errorf("%s on an empty list: %v", mode, err)
		}
	}
}

// The planner types a fanned-out bot's outputs as list<T>, and the join
// turns that back into what the target port declares. This is the exact
// snap that get-paid needed and could not express.
func TestAFannedOutBotCanFeedAScalarPortThroughAJoin(t *testing.T) {
	rs := resolvedTwoBotSwarm(t, schema.Snap{
		From: "sender.acted_on", To: "notifier.message", Join: "lines",
	})
	checks := TypeCheckSnaps(rs)

	var joined *SnapCheck
	for i := range checks {
		if checks[i].Snap.Join != "" {
			joined = &checks[i]
		}
	}
	if joined == nil {
		t.Fatal("the joined snap was not checked")
	}
	if !joined.OK {
		t.Fatalf("join snap failed to type-check: %v", joined.Err)
	}
	if joined.FromType.String() != "string" {
		t.Errorf("FromType after join = %s, want string", joined.FromType)
	}

	// And without the join it must still fail, or the join isn't doing
	// anything and the test proves nothing.
	rs = resolvedTwoBotSwarm(t, schema.Snap{From: "sender.acted_on", To: "notifier.message"})
	for _, c := range TypeCheckSnaps(rs) {
		if c.Snap.To == "notifier.message" && c.OK {
			t.Error("list<string> -> string type-checked without a join")
		}
	}
}

// resolvedTwoBotSwarm builds the shape get-paid has: a chaser whose
// `drafted` list fans a sender out, and a notifier downstream of it. Built
// from the real catalog rather than hand-assembled, so the types under test
// are the ones bots actually declare.
func resolvedTwoBotSwarm(t *testing.T, downstream schema.Snap) *ResolvedSwarm {
	t.Helper()
	root := repoRootForLevels(t)
	yaml := `apiVersion: nanobots.dev/v1alpha1
kind: Nanoswarm
metadata:
  name: join-probe
  description: probe
spec:
  bots:
    - id: chaser
      use: invoice-chaser@0.1.0
    - id: sender
      use: email-send-approved@0.1.0
    - id: notifier
      use: notify@0.1.0
      inputs:
        channel: "slack:#x"
  snaps:
    - from: chaser.drafted.*.draft_id
      to: sender.draft_id
    - from: chaser.drafted.*.subject
      to: sender.summary
    - from: ` + downstream.From + `
      to: ` + downstream.To + `
`
	if downstream.Join != "" {
		yaml += "      join: " + downstream.Join + "\n"
	}
	yaml += `  deploy:
    target: local
`
	path := filepath.Join(t.TempDir(), "probe.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	sw, err := schema.LoadNanoswarm(path)
	if err != nil {
		t.Fatal(err)
	}
	rs, err := Resolve(sw, filepath.Join(root, "bots"))
	if err != nil {
		t.Fatal(err)
	}
	return rs
}
