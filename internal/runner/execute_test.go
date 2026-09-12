package runner

import (
	"testing"

	"github.com/redbotster/nanobots/internal/schema"
	"os"
	"path/filepath"
)

func TestNeedsRealPDFRender(t *testing.T) {
	cases := []struct {
		name  string
		steps []schema.Step
		want  bool
	}{
		{"no render step", []schema.Step{{Type: "service.call"}}, false},
		{"render to html", []schema.Step{{Type: "transform.render", To: "html"}}, false},
		{"render to pdf", []schema.Step{{Type: "transform.render", To: "pdf"}}, true},
		// png needs Chrome exactly as much as pdf does; this used to be
		// missed, so a bare bot rendering a chart would have got an image
		// with no browser in it. sheet-reporter renders one for real.
		{"render to png", []schema.Step{{Type: "transform.render", To: "png"}}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			nb := &schema.Nanobot{Spec: schema.NanobotSpec{Steps: c.steps}}
			if got := needsBrowser(nb); got != c.want {
				t.Errorf("needsBrowser = %v, want %v", got, c.want)
			}
		})
	}
}

// The image a bot runs in is chosen from what its steps actually do, not
// from the harness name it declares. Fifteen of the nineteen bots declaring
// "openclaw" never render anything — ai.generate is an HTTP callback to
// nanobotd, so the container needs nothing but the interpreter — and each
// was pulling a 1.1GB Chromium image to make an HTTP request.
func TestImageForPicksByWhatTheBotActuallyDoes(t *testing.T) {
	llmOnly := []schema.Step{{Type: "ai.generate"}, {Type: "service.call"}}
	renders := []schema.Step{{Type: "ai.generate"}, {Type: "transform.render", To: "pdf"}}
	pngOnly := []schema.Step{{Type: "transform.render", To: "png"}}

	for _, tc := range []struct {
		name, declared, want string
		steps                []schema.Step
	}{
		{"openclaw bot that never renders drops to bare", "openclaw", "bare", llmOnly},
		{"openclaw bot that renders keeps openclaw", "openclaw", "openclaw", renders},
		{"bare bot that renders is promoted", "bare", "openclaw", renders},
		{"bare bot rendering a png is promoted too", "bare", "openclaw", pngOnly},
		{"bare bot that doesn't render stays bare", "bare", "bare", llmOnly},
	} {
		t.Run(tc.name, func(t *testing.T) {
			nb := &schema.Nanobot{Spec: schema.NanobotSpec{
				Harness: schema.Harness{Type: tc.declared},
				Steps:   tc.steps,
			}}
			if got := imageFor(nb, NewRun("swarm"), "bot"); got != tc.want {
				t.Errorf("imageFor(%s bot) = %q, want %q", tc.declared, got, tc.want)
			}
		})
	}
}

// Measured against the real catalog: if this ratio moves, the size claim in
// docs/harnesses.md moves with it.
func TestMostCatalogBotsDoNotNeedABrowser(t *testing.T) {
	botsDir := filepath.Join(repoRootForTest(t), "bots")
	entries, err := os.ReadDir(botsDir)
	if err != nil {
		t.Fatal(err)
	}
	var browser, lean []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		nb, err := schema.LoadNanobot(filepath.Join(botsDir, e.Name(), "nanobot.yaml"))
		if err != nil {
			t.Fatalf("load %s: %v", e.Name(), err)
		}
		if imageFor(nb, NewRun("s"), e.Name()) == "openclaw" {
			browser = append(browser, e.Name())
		} else {
			lean = append(lean, e.Name())
		}
	}
	t.Logf("%d/%d bots run on the 25MB bare image; these %d need the 1.1GB one: %v",
		len(lean), len(lean)+len(browser), len(browser), browser)
	if len(browser) > 8 {
		t.Errorf("%d bots need a browser — that's more than expected; has a render step crept in?", len(browser))
	}
}
