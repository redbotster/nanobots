package runner

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/redbotster/nanobots/internal/schema"
)

func repoRootForTest(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// this file: internal/runner/agentneed_test.go
	return filepath.Join(filepath.Dir(file), "..", "..")
}

func TestNeedsOneClawAgent(t *testing.T) {
	for _, tc := range []struct {
		name string
		spec schema.NanobotSpec
		want bool
	}{
		{
			name: "ai.generate needs Shroud, which is the agent's credential",
			spec: schema.NanobotSpec{Steps: []schema.Step{{Type: "ai.generate"}}},
			want: true,
		},
		{
			name: "memory is namespaced per agent",
			spec: schema.NanobotSpec{Steps: []schema.Step{{Type: "memory.get"}}},
			want: true,
		},
		{
			name: "memory.put counts too, not just get",
			spec: schema.NanobotSpec{Steps: []schema.Step{{Type: "memory.put"}}},
			want: true,
		},
		{
			// Found running lead-enricher@0.2.0 for real: a bot whose only
			// model-calling step is agent.loop (no ai.generate at all)
			// silently got no agent, GenerateWithTools fell back to a
			// direct provider key with no tool-calling, and the step failed
			// immediately with llm.ErrNoToolCalling. DemoDeps never
			// reaches this function, so no unit test had caught it.
			name: "agent.loop needs Shroud exactly like ai.generate does",
			spec: schema.NanobotSpec{Steps: []schema.Step{{Type: "agent.loop"}}},
			want: true,
		},
		{
			name: "a purely deterministic pipeline needs nothing",
			spec: schema.NanobotSpec{Steps: []schema.Step{
				{Type: "transform.render"}, {Type: "transform.pick"}, {Type: "notify"},
			}},
			want: false,
		},
		{
			// Not because approvals are local-only by design — the approver
			// does try to mirror them to 1Claw — but because that call is
			// refused whichever credential it is given, so paying an agent
			// slot for it would buy a logged error. See approver.go's
			// mirror() for the two refusals.
			name: "an approval gets no agent, because the mirror cannot work anyway",
			spec: schema.NanobotSpec{Steps: []schema.Step{{Type: "approve"}}},
			want: false,
		},
		{
			name: "a demo service calls fixtures, not 1Claw",
			spec: schema.NanobotSpec{
				Services: []schema.Service{{ID: "erp", Provider: "acme", Connection: schema.ConnectionDemo}},
				Steps:    []schema.Step{{Type: "service.call", Service: "erp"}},
			},
			want: false,
		},
		{
			name: "an unset connection defaults to demo",
			spec: schema.NanobotSpec{
				Services: []schema.Service{{ID: "erp", Provider: "acme"}},
				Steps:    []schema.Step{{Type: "service.call", Service: "erp"}},
			},
			want: false,
		},
		{
			name: "a live service with a native client here bypasses 1Claw entirely",
			spec: schema.NanobotSpec{
				Services: []schema.Service{{ID: "gmail", Provider: "google", Connection: schema.ConnectionOAuthNative}},
				Steps:    []schema.Step{{Type: "service.call", Service: "gmail"}},
			},
			want: false,
		},
		{
			name: "a live service with no native client goes through 1Claw's generic binding",
			spec: schema.NanobotSpec{
				Services: []schema.Service{{ID: "erp", Provider: "acme", Connection: schema.ConnectionOAuthNative}},
				Steps:    []schema.Step{{Type: "service.call", Service: "erp"}},
			},
			want: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			nb := &schema.Nanobot{Spec: tc.spec}
			if got := needsOneClawAgent(nb); got != tc.want {
				t.Errorf("needsOneClawAgent = %v, want %v", got, tc.want)
			}
		})
	}
}

// The point of the change, measured against the real catalog rather than
// invented specs: a third of it should stop consuming agent slots. If this
// number moves, the cap math in docs/oneclaw-bridge.md moves with it.
func TestRealCatalogNeedsFarFewerAgentsThanItHasBots(t *testing.T) {
	botsDir := filepath.Join(repoRootForTest(t), "bots")
	entries, err := os.ReadDir(botsDir)
	if err != nil {
		t.Fatalf("read bots dir: %v", err)
	}

	var needs, free []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		nb, err := schema.LoadNanobot(filepath.Join(botsDir, e.Name(), "nanobot.yaml"))
		if err != nil {
			t.Fatalf("load %s: %v", e.Name(), err)
		}
		if needsOneClawAgent(nb) {
			needs = append(needs, e.Name())
		} else {
			free = append(free, e.Name())
		}
	}

	if len(free) == 0 {
		t.Fatal("no bot avoids needing an agent — the predicate is doing nothing")
	}
	t.Logf("%d/%d bots need a 1Claw agent; these %d do not: %v",
		len(needs), len(needs)+len(free), len(free), free)

	// Every one of these is a deterministic bot with no LLM step: if one
	// ever starts needing an agent, that's a real design change worth
	// noticing here rather than discovering at the agent cap.
	//
	for _, name := range []string{"notify", "render-pdf", "drive-save", "post-publisher", "email-send-approved"} {
		nb, err := schema.LoadNanobot(filepath.Join(botsDir, name, "nanobot.yaml"))
		if err != nil {
			t.Fatalf("load %s: %v", name, err)
		}
		if needsOneClawAgent(nb) {
			t.Errorf("%s now needs a 1Claw agent; it never used to", name)
		}
	}
}

// Approvals are answerable in this app only, and the code should not
// pretend otherwise.
//
// RunQueueApprover.mirror tries to open the same approval in 1Claw so it can
// be answered from a phone. It cannot: the Human API key is refused with 403
// "Only agents can request approvals", and an agent's own ocv_ key is not
// valid on api.1claw.co at all. Granting approving bots an agent was tried
// and changes nothing except the error in the log.
//
// This test exists so that stops being rediscovered. If it starts failing,
// 1Claw has probably gained an agent-authenticated approval request — in
// which case adding "approve" to needsOneClawAgent is the change, and the
// comments in approver.go, docs/oneclaw-bridge.md and the "nobody answered"
// remedy all need their caveats removed.
func TestApprovingBotsGetNoAgentBecauseTheMirrorCannotWork(t *testing.T) {
	botsDir := filepath.Join(repoRootForTest(t), "bots")
	entries, err := os.ReadDir(botsDir)
	if err != nil {
		t.Fatalf("read bots dir: %v", err)
	}

	var approvers []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		nb, err := schema.LoadNanobot(filepath.Join(botsDir, e.Name(), "nanobot.yaml"))
		if err != nil {
			t.Fatalf("load %s: %v", e.Name(), err)
		}
		asks := false
		for _, st := range nb.Spec.Steps {
			if st.Type == "approve" {
				asks = true
				break
			}
		}
		if !asks {
			continue
		}
		approvers = append(approvers, e.Name())
		// An approving bot may still need an agent for another reason; what
		// must not happen is the approval itself buying one.
		if needsOneClawAgent(nb) {
			onlyForApproval := true
			for _, st := range nb.Spec.Steps {
				if st.Type == "ai.generate" || strings.HasPrefix(st.Type, "memory.") {
					onlyForApproval = false
				}
			}
			if onlyForApproval {
				t.Errorf("%s gets a 1Claw agent only because it asks for approval, and that "+
					"approval cannot reach 1Claw — see approver.go mirror()", e.Name())
			}
		}
	}
	if len(approvers) == 0 {
		t.Fatal("no bot in the catalog asks for approval — this test is checking nothing")
	}
	t.Logf("%d approving bots, none paying an agent slot for it: %v", len(approvers), approvers)
}
