package runner

import (
	"os"
	"path/filepath"
	"runtime"
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
			name: "a purely deterministic pipeline needs nothing",
			spec: schema.NanobotSpec{Steps: []schema.Step{
				{Type: "transform.render"}, {Type: "transform.pick"}, {Type: "notify"},
			}},
			want: false,
		},
		{
			name: "approvals go through the local run queue, never 1Claw's",
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
