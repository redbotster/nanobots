package runner

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/redbotster/nanobots/internal/schema"
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

// The `llm` harness value: fixed steps that call an LLM, no browser. It
// exists because neither existing value described the fifteen bots that
// were declaring `openclaw` for an HTTP callback — `bare` is documented as
// "no LLM loop", which is a worse description, not a better one.
func TestLLMHarnessRunsOnTheSmallImage(t *testing.T) {
	llmBot := &schema.Nanobot{Spec: schema.NanobotSpec{
		Harness: schema.Harness{Type: "llm"},
		Steps:   []schema.Step{{Type: "ai.generate"}},
	}}
	if got := imageFor(llmBot, NewRun("s"), "bot"); got != "llm" {
		t.Errorf("imageFor(llm bot) = %q, want it left alone", got)
	}
	// And "llm" resolves to the same 25MB image bare does, because
	// ai.generate never runs in the container.
	//
	// Read off harnessBuild rather than through EnsureHarnessImage, which
	// is what this used to do. That call builds the image when it is
	// missing, so the test passed here only because a previous run had
	// already built it — and on any machine without one it tried to build
	// from repoRoot ".", which is this package's directory, and failed with
	// "lstat harness: no such file or directory". CI found it on its first
	// run. The claim is about the mapping, so the mapping is what to assert;
	// it needs no Docker and no repo root.
	llm, ok := harnessBuild["llm"]
	if !ok {
		t.Fatal("no llm harness in harnessBuild")
	}
	bare := harnessBuild["bare"]
	if llm.Tag != bare.Tag || llm.User != bare.User {
		t.Errorf("llm resolves to %s/%s, want the same as bare (%s/%s)",
			llm.Tag, llm.User, bare.Tag, bare.User)
	}
	if llm.Dockerfile != bare.Dockerfile {
		t.Errorf("llm builds from %s, bare from %s — same image means same Dockerfile",
			llm.Dockerfile, bare.Dockerfile)
	}
}

// An llm bot that does render is still promoted — the declaration says what
// the bot is, the steps say what it needs, and need wins.
func TestLLMBotThatRendersIsPromoted(t *testing.T) {
	nb := &schema.Nanobot{Spec: schema.NanobotSpec{
		Harness: schema.Harness{Type: "llm"},
		Steps:   []schema.Step{{Type: "ai.generate"}, {Type: "transform.render", To: "pdf"}},
	}}
	if got := imageFor(nb, NewRun("s"), "bot"); got != "openclaw" {
		t.Errorf("imageFor = %q, want openclaw for a bot that renders", got)
	}
}

// A fanned-out bot's items are independent by definition, and used to run
// one after another anyway. Measured on a real supervisor-review run:
// three panel reviews of the same work, 12.5s + 7.1s + 6.9s, sequentially,
// out of a 50s run in which every second was an ai.generate call.
//
// These cover the parts that are easy to get wrong and invisible when they
// are: pairing, ordering, and what happens after a failure.
func TestFanOutItemsKeepTheirOwnResults(t *testing.T) {
	// Deliberately finishing in reverse order, so anything appending by
	// completion rather than assigning by index pairs the wrong output with
	// the wrong item — the exact bug that made runBotOnce return its
	// outputs instead of stashing them under the bot's name.
	out, errs := runItems(5, 5, func(i int) (map[string]any, error) {
		time.Sleep(time.Duration(5-i) * 10 * time.Millisecond)
		return map[string]any{"n": i}, nil
	})
	for i := range out {
		if errs[i] != nil {
			t.Fatalf("item %d: %v", i, errs[i])
		}
		if out[i]["n"] != i {
			t.Errorf("slot %d holds item %v — results and items came apart", i, out[i]["n"])
		}
	}
}

// Item i is not launched until i+1-limit items have finished, because that
// is when a slot frees up. It is what keeps a run log roughly in order —
// item 17 cannot appear before item 2 — and it is the honest version of a
// claim I first wrote as "item i starts before item i+1", which is not true
// once two goroutines are live and failed on the first run.
func TestNoItemJumpsAheadOfItsSlot(t *testing.T) {
	for _, limit := range []int{1, 2, 4} {
		var mu sync.Mutex
		done := 0
		var jumped []string
		runItems(12, limit, func(i int) (map[string]any, error) {
			mu.Lock()
			if want := i + 1 - limit; done < want {
				jumped = append(jumped, fmt.Sprintf("item %d began with %d finished, want >= %d", i, done, want))
			}
			mu.Unlock()
			time.Sleep(3 * time.Millisecond)
			mu.Lock()
			done++
			mu.Unlock()
			return nil, nil
		})
		if len(jumped) > 0 {
			t.Errorf("limit %d: %v", limit, jumped)
		}
	}
}

// A limit of 1 is the loop this replaced: one at a time, in order. The env
// var documents exactly that, and it is how the two are compared.
func TestALimitOfOneIsStrictlySequential(t *testing.T) {
	var mu sync.Mutex
	inFlight, peak := 0, 0
	var order []int
	runItems(6, 1, func(i int) (map[string]any, error) {
		mu.Lock()
		order = append(order, i)
		inFlight++
		if inFlight > peak {
			peak = inFlight
		}
		mu.Unlock()
		time.Sleep(5 * time.Millisecond)
		mu.Lock()
		inFlight--
		mu.Unlock()
		return nil, nil
	})
	if peak != 1 {
		t.Errorf("peak concurrency %d at a limit of 1", peak)
	}
	for i, got := range order {
		if got != i {
			t.Errorf("a limit of 1 ran items %v — it must be the loop it replaced", order)
			break
		}
	}
}

func TestFanOutRespectsTheLimit(t *testing.T) {
	var mu sync.Mutex
	inFlight, peak := 0, 0
	runItems(20, 4, func(i int) (map[string]any, error) {
		mu.Lock()
		inFlight++
		if inFlight > peak {
			peak = inFlight
		}
		mu.Unlock()
		time.Sleep(10 * time.Millisecond)
		mu.Lock()
		inFlight--
		mu.Unlock()
		return nil, nil
	})
	if peak > 4 {
		t.Errorf("peak concurrency %d, want at most 4 — twenty invoices is twenty model calls", peak)
	}
	if peak < 2 {
		t.Errorf("peak concurrency %d, so nothing actually ran at once", peak)
	}
}

// A fanned-out bot is usually sending something, so a failure has to stop
// the items that have not begun. The ones already in flight are left to
// finish: cancelling one mid-container would orphan that container.
func TestAFailedItemStopsTheOnesNotStartedYet(t *testing.T) {
	var mu sync.Mutex
	var ran []int
	_, errs := runItems(20, 2, func(i int) (map[string]any, error) {
		mu.Lock()
		ran = append(ran, i)
		mu.Unlock()
		if i == 0 {
			return nil, errors.New("the credential expired")
		}
		time.Sleep(5 * time.Millisecond)
		return nil, nil
	})
	if errs[0] == nil {
		t.Fatal("item 0 was supposed to fail")
	}
	mu.Lock()
	n := len(ran)
	mu.Unlock()
	// Item 0 fails, item 1 may already be in flight beside it, and a third
	// can be starting as the failure is recorded. Twenty would mean the
	// stop did nothing.
	if n > 4 {
		t.Errorf("%d of 20 items ran after the first one failed", n)
	}
}

// The reported failure is the lowest-numbered one, so the message is the
// same one the sequential version produced rather than whichever item lost
// a race.
func TestTheFirstItemsFailureIsTheOneReported(t *testing.T) {
	_, errs := runItems(4, 4, func(i int) (map[string]any, error) {
		if i >= 1 {
			time.Sleep(time.Duration(4-i) * 5 * time.Millisecond)
			return nil, fmt.Errorf("item %d broke", i)
		}
		time.Sleep(30 * time.Millisecond)
		return nil, errors.New("item 0 broke")
	})
	var first error
	for _, err := range errs {
		if err != nil {
			first = err
			break
		}
	}
	if first == nil || first.Error() != "item 0 broke" {
		t.Errorf("reported %v, want item 0's failure", first)
	}
}
