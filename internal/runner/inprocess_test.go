package runner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/redbotster/nanobots/internal/schema"
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
			got, why := runsInProcess(nb)
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
		if ok, _ := runsInProcess(nb); ok {
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
		if _, why := runsInProcess(nb); !strings.Contains(why, "browser") && !strings.Contains(why, "openclaw") {
			t.Errorf("%s needs a container for an unexpected reason: %s", name, why)
		}
	}
}
