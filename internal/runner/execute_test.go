package runner

import (
	"testing"

	"github.com/redbotster/nanobots/internal/schema"
	"os"
	"path/filepath"
	"strings"
	"time"
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

func TestAggregateOutputsBuildsOneListPerPort(t *testing.T) {
	nb := &schema.Nanobot{Spec: schema.NanobotSpec{Ports: schema.Ports{Outputs: []schema.OutputPort{
		{Name: "posts", Type: "json"},
		{Name: "sent_at", Type: "datetime"},
	}}}}

	got := aggregateOutputs(nb, []map[string]any{
		{"posts": "a", "sent_at": "t1"},
		{"posts": "b", "sent_at": "t2"},
	})
	// Every port becomes a list, because the bot ran twice — that's what
	// the planner promised anything downstream (resolveEndpointType).
	if posts, ok := got["posts"].([]any); !ok || len(posts) != 2 || posts[0] != "a" || posts[1] != "b" {
		t.Errorf("posts = %#v, want the two values in order", got["posts"])
	}
	if sent, ok := got["sent_at"].([]any); !ok || len(sent) != 2 {
		t.Errorf("sent_at = %#v, want two values", got["sent_at"])
	}
}

// "For each overdue invoice" over no overdue invoices is a successful
// no-op, not a failure — a quiet week shouldn't be a red run. Downstream
// still needs to see an output, so it's an empty list rather than nothing.
func TestEmptyFanOutProducesEmptyListsNotMissingOutputs(t *testing.T) {
	nb := &schema.Nanobot{Spec: schema.NanobotSpec{Ports: schema.Ports{Outputs: []schema.OutputPort{
		{Name: "message_id", Type: "string"},
	}}}}
	got := emptyListOutputs(nb)
	v, ok := got["message_id"].([]any)
	if !ok {
		t.Fatalf("message_id = %#v, want an empty list", got["message_id"])
	}
	if len(v) != 0 {
		t.Errorf("message_id = %#v, want it empty", v)
	}
}

// One approval covers the whole batch (the alternative — twenty prompts —
// means nobody reads them), so the summary has to make the scale
// unmissable: it's one click authorising twenty real sends.
func TestBatchApproverAsksOnceAndSaysHowMany(t *testing.T) {
	run := NewRun("get-paid")
	batch := &BatchApprover{Inner: &RunQueueApprover{Run: run, Bot: "sender", Step: "approve"}, Total: 20}

	results := make(chan bool, 3)
	for i := 0; i < 3; i++ {
		go func() {
			ok, _, _ := batch.Approve("Send a reminder to client@example.com?", "high")
			results <- ok
		}()
	}

	waitFor(t, func() bool { return len(run.PendingApprovals()) == 1 })
	pending := run.PendingApprovals()
	if n := len(pending); n != 1 {
		t.Fatalf("%d approvals opened, want exactly 1 for the batch", n)
	}
	if !strings.Contains(pending[0].Summary, "20 in total") {
		t.Errorf("summary = %q, want it to name the batch size", pending[0].Summary)
	}
	if !strings.Contains(pending[0].Summary, "client@example.com") {
		t.Errorf("summary = %q, want it to keep the bot's own wording", pending[0].Summary)
	}

	if err := run.Decide(pending[0].ID, true, "you"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		select {
		case ok := <-results:
			if !ok {
				t.Error("an item did not inherit the batch decision")
			}
		case <-time.After(5 * time.Second):
			t.Fatal("an item never got the batch decision")
		}
	}
	if n := len(run.PendingApprovals()); n != 0 {
		t.Errorf("%d approvals still open after deciding the batch", n)
	}
}

func TestBatchSummaryLeavesASingleItemAlone(t *testing.T) {
	if got := batchSummary("Send this?", 1); got != "Send this?" {
		t.Errorf("batchSummary(_, 1) = %q, want it unchanged", got)
	}
}
