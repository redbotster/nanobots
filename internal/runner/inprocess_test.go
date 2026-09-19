package runner

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/redbotster/nanobots/internal/planner"
	"github.com/redbotster/nanobots/internal/schema"
	"github.com/redbotster/nanobots/internal/step"
)

// A container earns its place when there is something in it worth
// isolating. For most of the catalog there is not: the steps are a declared
// list run by this repo's own interpreter, and every step that reaches the
// outside world already runs in nanobotd.
func TestWhichBotsStillNeedAContainer(t *testing.T) {
	for _, tc := range []struct {
		name string
		spec schema.NanobotSpec
		want bool
		why  string
	}{
		{
			name: "a deterministic pipeline needs nothing",
			spec: schema.NanobotSpec{
				Harness: schema.Harness{Type: "bare"},
				Steps:   []schema.Step{{Type: "transform.pick"}, {Type: "notify"}},
			},
			want: true,
		},
		{
			name: "an llm bot is a callback, not a sandbox problem",
			spec: schema.NanobotSpec{
				Harness: schema.Harness{Type: "llm"},
				Steps:   []schema.Step{{Type: "ai.generate"}},
			},
			want: true,
		},
		{
			name: "an approval gate holds a goroutine, not a container",
			spec: schema.NanobotSpec{
				Harness: schema.Harness{Type: "bare"},
				Steps:   []schema.Step{{Type: "approve"}, {Type: "service.call"}},
			},
			want: true,
		},
		{
			// Chromium executes pages nobody here wrote. That is the thing a
			// sandbox is actually for.
			name: "rendering to pdf drives a real browser",
			spec: schema.NanobotSpec{
				Harness: schema.Harness{Type: "bare"},
				Steps:   []schema.Step{{Type: "transform.render", To: "pdf"}},
			},
			want: false,
			why:  "headless browser",
		},
		{
			name: "declaring openclaw keeps the container even without a render step",
			spec: schema.NanobotSpec{
				Harness: schema.Harness{Type: "openclaw"},
				Steps:   []schema.Step{{Type: "transform.pick"}},
			},
			want: false,
			why:  "openclaw",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			nb := &schema.Nanobot{Spec: tc.spec}
			got, why := runsInProcess(nb, "")
			if got != tc.want {
				t.Fatalf("runsInProcess = %v (%q), want %v", got, why, tc.want)
			}
			if !got && !strings.Contains(why, tc.why) {
				t.Errorf("reason %q does not mention %q — the run log shows this", why, tc.why)
			}
			if got && why != "" {
				t.Errorf("an in-process bot should carry no reason, got %q", why)
			}
		})
	}
}

// v3 Phase 1's execution: override, validated as the runner sees it — plan
// time (internal/planner.CheckExecution) already rejected anything but ""
// or "container" by the time this runs, so runsInProcess itself only needs
// to answer "does either level say container", with the instance winning.
func TestExecutionOverrideForcesAContainer(t *testing.T) {
	bareNoRender := schema.NanobotSpec{
		Harness: schema.Harness{Type: "bare"},
		Steps:   []schema.Step{{Type: "transform.pick"}},
	}

	t.Run("no override runs in-process as usual", func(t *testing.T) {
		nb := &schema.Nanobot{Spec: bareNoRender}
		if ok, why := runsInProcess(nb, ""); !ok {
			t.Errorf("runsInProcess = false (%q), want true", why)
		}
	})

	t.Run("the bot's own harness.execution forces a container", func(t *testing.T) {
		spec := bareNoRender
		spec.Harness.Execution = "container"
		nb := &schema.Nanobot{Spec: spec}
		ok, why := runsInProcess(nb, "")
		if ok {
			t.Fatal("runsInProcess = true, want a forced container")
		}
		if !strings.Contains(why, "execution: container") || !strings.Contains(why, "the bot") {
			t.Errorf("reason %q does not explain it was the bot's own override", why)
		}
	})

	t.Run("a swarm's instance override forces a container even without one on the bot", func(t *testing.T) {
		nb := &schema.Nanobot{Spec: bareNoRender}
		ok, why := runsInProcess(nb, "container")
		if ok {
			t.Fatal("runsInProcess = true, want a forced container")
		}
		if !strings.Contains(why, "execution: container") || !strings.Contains(why, "this swarm") {
			t.Errorf("reason %q does not explain it was this swarm's override", why)
		}
	})

	t.Run("a swarm's instance override wins over the bot's own default", func(t *testing.T) {
		// Both say "container" here, so this only proves the instance path is
		// actually reached first rather than falling through to the bot's own
		// check — a real disagreement (instance clearing what the bot forces)
		// isn't legal, since CheckExecution has no "inprocess" value to clear
		// it with.
		spec := bareNoRender
		spec.Harness.Execution = "container"
		nb := &schema.Nanobot{Spec: spec}
		if ok, why := runsInProcess(nb, "container"); ok || !strings.Contains(why, "this swarm") {
			t.Errorf("runsInProcess = %v (%q), want a forced container attributed to this swarm", ok, why)
		}
	})
}

// The catalog's shape, asserted so a change to it is noticed here rather
// than by someone wondering why their laptop started needing Docker.
func TestMostOfTheCatalogNeedsNoContainer(t *testing.T) {
	botsDir := filepath.Join(repoRootForTest(t), "bots")
	entries, err := os.ReadDir(botsDir)
	if err != nil {
		t.Fatalf("read bots: %v", err)
	}
	var inProcess, containered []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		nb, err := schema.LoadNanobot(filepath.Join(botsDir, e.Name(), "nanobot.yaml"))
		if err != nil {
			t.Fatalf("load %s: %v", e.Name(), err)
		}
		if ok, _ := runsInProcess(nb, ""); ok {
			inProcess = append(inProcess, e.Name())
		} else {
			containered = append(containered, e.Name())
		}
	}
	t.Logf("%d of %d bots run in-process; these still need Docker: %v",
		len(inProcess), len(inProcess)+len(containered), containered)

	if len(inProcess) < len(containered) {
		t.Errorf("only %d of %d bots avoid a container — the in-process path has stopped applying to most of the catalog",
			len(inProcess), len(inProcess)+len(containered))
	}
	// Every bot still needing one must need it for the browser, which is
	// the only reason left. A new reason should be a deliberate change.
	for _, name := range containered {
		nb, _ := schema.LoadNanobot(filepath.Join(botsDir, name, "nanobot.yaml"))
		if _, why := runsInProcess(nb, ""); !strings.Contains(why, "browser") && !strings.Contains(why, "openclaw") {
			t.Errorf("%s needs a container for an unexpected reason: %s", name, why)
		}
	}
}

// v3 Phase 1 item 3: a two-bot swarm with one bot in each execution mode,
// checking the wire between them — not just that each bot's own mode
// resolves correctly (TestExecutionOverrideForcesAContainer already proves
// that), but that data crossing the boundary between an in-process bot and
// a forced-container one goes through the same resolveInputsAt path either
// way, with mode carrying no special case of its own.
//
// runBotOnceFn stands in for both bots — this package's tests never launch
// a real container (.github/workflows/ci.yml: "nothing here needs Docker"),
// and RunContainer itself is exercised live, not by go test. What this test
// can and does prove without Docker: runsInProcess sees the right override
// for each bot instance, and "down"'s real resolveInputsAt call — the
// actual wire — resolves "up"'s real recorded output through a real snap,
// regardless of which mode either bot is in.
func TestExecutionModesDoNotBreakTheWireBetweenTwoBots(t *testing.T) {
	up := schema.BotRef{ID: "up"}
	down := schema.BotRef{ID: "down", Execution: "container"}
	rs := &planner.ResolvedSwarm{
		Swarm: &schema.Nanoswarm{Spec: schema.NanoswarmSpec{
			Bots:  []schema.BotRef{up, down},
			Snaps: []schema.Snap{{From: "up.result", To: "down.value"}},
		}},
		Bots: map[string]*planner.ResolvedBot{
			"up": {Ref: up, Nanobot: &schema.Nanobot{
				Metadata: schema.Metadata{Name: "up", Version: "0.1.0"},
				Spec: schema.NanobotSpec{Ports: schema.Ports{
					Outputs: []schema.OutputPort{{Name: "result", Type: "string"}},
				}},
			}},
			"down": {Ref: down, Nanobot: &schema.Nanobot{
				Metadata: schema.Metadata{Name: "down", Version: "0.1.0"},
				Spec: schema.NanobotSpec{Ports: schema.Ports{
					Inputs: []schema.InputPort{{Name: "value", Type: "string"}},
				}},
			}},
		},
	}

	var upInProcess, downInProcess bool
	var downSawValue any
	var resolveErr error

	run := NewRun("probe")
	o := &Orchestrator{}
	o.runBotOnceFn = func(run *Run, rs *planner.ResolvedSwarm, botID string, rb *planner.ResolvedBot, at fanIndex, total int, batch step.Approver) (map[string]any, error) {
		switch botID {
		case "up":
			upInProcess, _ = runsInProcess(rb.Nanobot, rb.Ref.Execution)
			return map[string]any{"result": "hello from up"}, nil
		case "down":
			downInProcess, _ = runsInProcess(rb.Nanobot, rb.Ref.Execution)
			var inputs map[string]any
			inputs, resolveErr = o.resolveInputsAt(run, rs, botID, rb, at)
			downSawValue = inputs["value"]
			return map[string]any{}, nil
		}
		return nil, fmt.Errorf("unexpected bot %q", botID)
	}

	if err := o.runOneBot(run, rs, "up"); err != nil {
		t.Fatalf("up: %v", err)
	}
	if err := o.runOneBot(run, rs, "down"); err != nil {
		t.Fatalf("down: %v", err)
	}
	if resolveErr != nil {
		t.Fatalf("down's resolveInputsAt: %v", resolveErr)
	}

	if !upInProcess {
		t.Error("up should run in-process (no override, nothing needs a browser)")
	}
	if downInProcess {
		t.Error("down should have been forced into a container by its swarm-instance execution: override")
	}
	if downSawValue != "hello from up" {
		t.Errorf(`down's resolved "value" input = %v, want "hello from up" — the wire between the two modes is broken`, downSawValue)
	}
}
