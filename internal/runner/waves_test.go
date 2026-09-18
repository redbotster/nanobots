package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redbotster/nanobots/internal/oneclaw"
	"github.com/redbotster/nanobots/internal/planner"
	"github.com/redbotster/nanobots/internal/schema"
)

// resolvedFor builds the minimum ResolvedSwarm runLevels reads: it only
// looks bots up by id.
func resolvedFor(ids ...string) *planner.ResolvedSwarm {
	refs := make([]schema.BotRef, 0, len(ids))
	for _, id := range ids {
		refs = append(refs, schema.BotRef{ID: id})
	}
	return swarmWith(refs, nil)
}

// Bots in one wave have no path between them in the DAG, so they can run at
// the same time — which is the entire point. If they were still serialised
// the change would be a no-op with extra machinery, and nothing else here
// would catch that.
func TestBotsInAWaveRunAtTheSameTime(t *testing.T) {
	var running, peak int32
	var mu sync.Mutex

	o := &Orchestrator{runBotFn: func(_ *Run, _ *planner.ResolvedSwarm, _ string, _ *planner.ResolvedBot) error {
		n := atomic.AddInt32(&running, 1)
		mu.Lock()
		if n > peak {
			peak = n
		}
		mu.Unlock()
		time.Sleep(30 * time.Millisecond)
		atomic.AddInt32(&running, -1)
		return nil
	}}

	run := NewRun("probe")
	err := o.runLevels(run, resolvedFor("a", "b", "c"), [][]string{{"a", "b", "c"}})
	if err != nil {
		t.Fatal(err)
	}
	if peak < 2 {
		t.Errorf("peak concurrency was %d — the wave ran one bot at a time", peak)
	}
}

// The other half of the contract: a later wave must not start until the
// earlier one has finished, because that is the only thing stopping a bot
// from reading an output that doesn't exist yet.
func TestALaterWaveWaitsForTheEarlierOne(t *testing.T) {
	var mu sync.Mutex
	var order []string
	var inFlight int32
	var overlapped bool

	o := &Orchestrator{runBotFn: func(_ *Run, _ *planner.ResolvedSwarm, id string, _ *planner.ResolvedBot) error {
		mu.Lock()
		order = append(order, "start:"+id)
		mu.Unlock()
		atomic.AddInt32(&inFlight, 1)
		time.Sleep(20 * time.Millisecond)
		atomic.AddInt32(&inFlight, -1)
		return nil
	}}

	// second must never begin while either first-wave bot is still going.
	o.runBotFn = func(run *Run, rs *planner.ResolvedSwarm, id string, rb *planner.ResolvedBot) error {
		if id == "second" && atomic.LoadInt32(&inFlight) > 0 {
			mu.Lock()
			overlapped = true
			mu.Unlock()
		}
		atomic.AddInt32(&inFlight, 1)
		time.Sleep(20 * time.Millisecond)
		atomic.AddInt32(&inFlight, -1)
		mu.Lock()
		order = append(order, id)
		mu.Unlock()
		return nil
	}

	err := o.runLevels(NewRun("probe"), resolvedFor("a", "b", "second"),
		[][]string{{"a", "b"}, {"second"}})
	if err != nil {
		t.Fatal(err)
	}
	if overlapped {
		t.Error("a second-wave bot started while the first wave was still running")
	}
	if order[len(order)-1] != "second" {
		t.Errorf("order = %v, want second last", order)
	}
}

// When two bots in a wave fail, both errors matter. A swarm's independent
// branches usually fail for one shared reason — an expired credential, a
// stopped Docker — and reporting whichever lost the race sends someone
// looking for two bugs.
func TestEveryFailureInAWaveIsReported(t *testing.T) {
	o := &Orchestrator{runBotFn: func(_ *Run, _ *planner.ResolvedSwarm, id string, _ *planner.ResolvedBot) error {
		if id == "ok" {
			return nil
		}
		return fmt.Errorf("%s could not reach the vault", id)
	}}

	err := o.runLevels(NewRun("probe"), resolvedFor("alpha", "ok", "beta"),
		[][]string{{"alpha", "ok", "beta"}})
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{"alpha", "beta", "vault"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not mention %q: %v", want, err)
		}
	}
	// Deterministic ordering: the message must not depend on which
	// goroutine finished first.
	if a, b := strings.Index(err.Error(), "alpha"), strings.Index(err.Error(), "beta"); a > b {
		t.Errorf("failures are not in swarm order: %v", err)
	}
}

// A single failure has to read exactly as it always did — a swarm that
// fails one bot is the common case, and its message is what every existing
// test and every run in history looks like.
func TestASingleFailureReadsUnchanged(t *testing.T) {
	o := &Orchestrator{runBotFn: func(_ *Run, _ *planner.ResolvedSwarm, id string, _ *planner.ResolvedBot) error {
		if id == "bad" {
			return fmt.Errorf("container exited 1")
		}
		return nil
	}}

	err := o.runLevels(NewRun("probe"), resolvedFor("bad"), [][]string{{"bad"}})
	if err == nil || err.Error() != "container exited 1" {
		t.Errorf("err = %v, want the bot's own error verbatim", err)
	}

	// And the same when it fails alongside a healthy sibling.
	err = o.runLevels(NewRun("probe"), resolvedFor("bad", "good"), [][]string{{"bad", "good"}})
	if err == nil || err.Error() != "container exited 1" {
		t.Errorf("err = %v, want the bot's own error verbatim", err)
	}
}

// Cancelling a wave half-way would orphan a container that was already
// starting — a leak this runner has had once before. So a failing wave is
// allowed to finish, and only then does the run stop.
func TestAFailingWaveStillLetsItsSiblingsFinish(t *testing.T) {
	var finished int32
	o := &Orchestrator{runBotFn: func(_ *Run, _ *planner.ResolvedSwarm, id string, _ *planner.ResolvedBot) error {
		if id == "fails" {
			return fmt.Errorf("boom")
		}
		time.Sleep(30 * time.Millisecond)
		atomic.AddInt32(&finished, 1)
		return nil
	}}

	err := o.runLevels(NewRun("probe"), resolvedFor("fails", "slow1", "slow2"),
		[][]string{{"fails", "slow1", "slow2"}})
	if err == nil {
		t.Fatal("expected an error")
	}
	if got := atomic.LoadInt32(&finished); got != 2 {
		t.Errorf("%d siblings finished, want 2 — a failure cancelled work mid-container", got)
	}
}

// A later wave must not run after an earlier one failed.
func TestNoLaterWaveRunsAfterAFailure(t *testing.T) {
	var ranLater bool
	o := &Orchestrator{runBotFn: func(_ *Run, _ *planner.ResolvedSwarm, id string, _ *planner.ResolvedBot) error {
		if id == "later" {
			ranLater = true
		}
		if id == "first" {
			return fmt.Errorf("boom")
		}
		return nil
	}}

	if err := o.runLevels(NewRun("probe"), resolvedFor("first", "later"),
		[][]string{{"first"}, {"later"}}); err == nil {
		t.Fatal("expected an error")
	}
	if ranLater {
		t.Error("a later wave ran after an earlier one failed")
	}
}

// Concurrency is capped because each bot is a container: four
// Chromium-bearing ones already want a couple of gigabytes, and a laptop
// that starts swapping finishes slower than it would have sequentially.
func TestConcurrencyIsCapped(t *testing.T) {
	var running, peak int32
	var mu sync.Mutex
	o := &Orchestrator{runBotFn: func(_ *Run, _ *planner.ResolvedSwarm, _ string, _ *planner.ResolvedBot) error {
		n := atomic.AddInt32(&running, 1)
		mu.Lock()
		if n > peak {
			peak = n
		}
		mu.Unlock()
		time.Sleep(15 * time.Millisecond)
		atomic.AddInt32(&running, -1)
		return nil
	}}

	var wide []string
	for i := 0; i < maxParallelBots*3; i++ {
		wide = append(wide, fmt.Sprintf("bot%02d", i))
	}
	if err := o.runLevels(NewRun("probe"), resolvedFor(wide...), [][]string{wide}); err != nil {
		t.Fatal(err)
	}
	if int(peak) > maxParallelBots {
		t.Errorf("peak concurrency was %d, cap is %d", peak, maxParallelBots)
	}
	if peak < 2 {
		t.Errorf("peak concurrency was %d — nothing ran in parallel at all", peak)
	}
}

// swarmWith builds a ResolvedSwarm with real bot refs and snaps, so
// runLevels sees the on_error values and dependency edges it reads.
func swarmWith(bots []schema.BotRef, snaps []schema.Snap) *planner.ResolvedSwarm {
	rs := &planner.ResolvedSwarm{
		Swarm: &schema.Nanoswarm{Spec: schema.NanoswarmSpec{Bots: bots, Snaps: snaps}},
		Bots:  map[string]*planner.ResolvedBot{},
	}
	for _, b := range bots {
		rs.Bots[b.ID] = &planner.ResolvedBot{Nanobot: &schema.Nanobot{
			Metadata: schema.Metadata{Name: b.ID, Version: "0.1.0"},
		}}
	}
	return rs
}

// The failure this exists for: get-paid sent every reminder, then failed
// the whole run because it couldn't post a Slack summary nothing reads.
func TestASwarmContinuesPastABotMarkedContinue(t *testing.T) {
	run := NewRun("get-paid")
	o := &Orchestrator{runBotFn: func(_ *Run, _ *planner.ResolvedSwarm, id string, _ *planner.ResolvedBot) error {
		if id == "notifier" {
			return fmt.Errorf("Secret slack/bot_token not found")
		}
		return nil
	}}
	rs := swarmWith([]schema.BotRef{
		{ID: "sender"},
		{ID: "notifier", OnError: schema.OnErrorContinue},
	}, nil)

	if err := o.runLevels(run, rs, [][]string{{"sender"}, {"notifier"}}); err != nil {
		t.Fatalf("the run failed despite on_error: continue: %v", err)
	}
	tol := run.GetTolerated()
	if len(tol) != 1 || tol[0].Bot != "notifier" {
		t.Fatalf("tolerated = %+v, want the notifier recorded", tol)
	}
	if !strings.Contains(tol[0].Error, "slack") {
		t.Errorf("the reason was lost: %q", tol[0].Error)
	}
}

// Default is stop. Silence must mean "fail loudly" — continuing has to be
// something someone chose, not something they got.
func TestTheDefaultIsStillToFailTheRun(t *testing.T) {
	o := &Orchestrator{runBotFn: func(_ *Run, _ *planner.ResolvedSwarm, id string, _ *planner.ResolvedBot) error {
		return fmt.Errorf("boom")
	}}
	for _, policy := range []string{"", schema.OnErrorStop} {
		run := NewRun("probe")
		rs := swarmWith([]schema.BotRef{{ID: "only", OnError: policy}}, nil)
		if err := o.runLevels(run, rs, [][]string{{"only"}}); err == nil {
			t.Errorf("on_error=%q did not fail the run", policy)
		}
		if len(run.GetTolerated()) != 0 {
			t.Errorf("on_error=%q recorded a tolerated failure", policy)
		}
	}
}

// Continuing past a bot does not mean pretending it produced anything.
// Without this, one tolerated failure cascades into a run of "upstream bot
// has no recorded outputs yet" from every bot behind it.
func TestBotsDownstreamOfAToleratedFailureAreSkipped(t *testing.T) {
	var ran []string
	var mu sync.Mutex
	o := &Orchestrator{runBotFn: func(_ *Run, _ *planner.ResolvedSwarm, id string, _ *planner.ResolvedBot) error {
		mu.Lock()
		ran = append(ran, id)
		mu.Unlock()
		if id == "middle" {
			return fmt.Errorf("boom")
		}
		return nil
	}}
	rs := swarmWith(
		[]schema.BotRef{
			{ID: "first"},
			{ID: "middle", OnError: schema.OnErrorContinue},
			{ID: "last"},
			{ID: "unrelated"},
		},
		[]schema.Snap{
			{From: "first.out", To: "middle.in"},
			{From: "middle.out", To: "last.in"},
		},
	)

	run := NewRun("probe")
	if err := o.runLevels(run, rs, [][]string{{"first"}, {"middle", "unrelated"}, {"last"}}); err != nil {
		t.Fatalf("run failed: %v", err)
	}
	for _, id := range ran {
		if id == "last" {
			t.Error("a bot downstream of a failed one was run against missing inputs")
		}
	}
	// A bot that never needed it still runs — skipping is about the data,
	// not about the wave.
	var sawUnrelated bool
	for _, id := range ran {
		if id == "unrelated" {
			sawUnrelated = true
		}
	}
	if !sawUnrelated {
		t.Error("an unrelated bot was skipped too")
	}
	// And the skip is in the log, not silent.
	var said bool
	for _, l := range run.LogEntries() {
		if l.Bot == "last" && strings.Contains(l.Msg, "skipped") {
			said = true
		}
	}
	if !said {
		t.Errorf("nothing in the log says why 'last' never ran: %+v", run.LogEntries())
	}
}

// Skipping is transitive: a bot behind a skipped bot is skipped too,
// without needing its own edge to the original failure.
func TestSkippingIsTransitive(t *testing.T) {
	var ran []string
	o := &Orchestrator{runBotFn: func(_ *Run, _ *planner.ResolvedSwarm, id string, _ *planner.ResolvedBot) error {
		ran = append(ran, id)
		if id == "a" {
			return fmt.Errorf("boom")
		}
		return nil
	}}
	rs := swarmWith(
		[]schema.BotRef{{ID: "a", OnError: schema.OnErrorContinue}, {ID: "b"}, {ID: "c"}},
		[]schema.Snap{{From: "a.out", To: "b.in"}, {From: "b.out", To: "c.in"}},
	)
	if err := o.runLevels(NewRun("probe"), rs, [][]string{{"a"}, {"b"}, {"c"}}); err != nil {
		t.Fatal(err)
	}
	if len(ran) != 1 || ran[0] != "a" {
		t.Errorf("ran = %v, want only a", ran)
	}
}

// whenSwarm builds a two-bot swarm for testing when: gating directly,
// rather than through swarmWith — it needs a declared input port and a real
// Ref, which swarmWith's bare-bones ResolvedBot doesn't carry.
func whenSwarm(gateWhen string, gateAmount any) *planner.ResolvedSwarm {
	gate := schema.BotRef{ID: "gate", When: gateWhen, Inputs: map[string]any{"amount": gateAmount}}
	notify := schema.BotRef{ID: "notify"}
	return &planner.ResolvedSwarm{
		Swarm: &schema.Nanoswarm{Spec: schema.NanoswarmSpec{
			Bots:  []schema.BotRef{gate, notify},
			Snaps: []schema.Snap{{From: "gate.ok", To: "notify.ok"}},
		}},
		Bots: map[string]*planner.ResolvedBot{
			"gate": {Ref: gate, Nanobot: &schema.Nanobot{
				Metadata: schema.Metadata{Name: "gate", Version: "0.1.0"},
				Spec: schema.NanobotSpec{Ports: schema.Ports{
					Inputs:  []schema.InputPort{{Name: "amount", Type: "string", Required: true}},
					Outputs: []schema.OutputPort{{Name: "ok", Type: "string"}},
				}},
			}},
			"notify": {Ref: notify, Nanobot: &schema.Nanobot{
				Metadata: schema.Metadata{Name: "notify", Version: "0.1.0"},
				Spec: schema.NanobotSpec{Ports: schema.Ports{
					Inputs: []schema.InputPort{{Name: "ok", Type: "string", Required: true}},
				}},
			}},
		},
	}
}

// A when: this bot's own input resolves false skips exactly the way
// on_error: continue does — the bot never runs, and neither does anything
// downstream of it, because its output never arrived.
func TestWhenFalseSkipsTheBotAndDownstream(t *testing.T) {
	var ran []string
	o := &Orchestrator{runBotFn: func(_ *Run, _ *planner.ResolvedSwarm, id string, _ *planner.ResolvedBot) error {
		ran = append(ran, id)
		return nil
	}}
	rs := whenSwarm("{{inputs.amount}} > 500", 100)

	run := NewRun("probe")
	if err := o.runLevels(run, rs, [][]string{{"gate"}, {"notify"}}); err != nil {
		t.Fatalf("when: false failed the run: %v", err)
	}
	if len(ran) != 0 {
		t.Errorf("ran %v, want nothing — when: was false", ran)
	}
	var said bool
	for _, l := range run.LogEntries() {
		if l.Bot == "gate" && strings.Contains(l.Msg, "when:") && strings.Contains(l.Msg, "false") {
			said = true
		}
	}
	if !said {
		t.Errorf("nothing in the log says when: was false: %+v", run.LogEntries())
	}
}

func TestWhenTrueRunsTheBot(t *testing.T) {
	var ran []string
	o := &Orchestrator{runBotFn: func(_ *Run, _ *planner.ResolvedSwarm, id string, _ *planner.ResolvedBot) error {
		ran = append(ran, id)
		return nil
	}}
	rs := whenSwarm("{{inputs.amount}} > 500", 750)

	run := NewRun("probe")
	if err := o.runLevels(run, rs, [][]string{{"gate"}, {"notify"}}); err != nil {
		t.Fatalf("the run failed: %v", err)
	}
	if len(ran) != 2 {
		t.Errorf("ran %v, want both gate and notify", ran)
	}
}

func TestNoWhenAlwaysRuns(t *testing.T) {
	var ran []string
	o := &Orchestrator{runBotFn: func(_ *Run, _ *planner.ResolvedSwarm, id string, _ *planner.ResolvedBot) error {
		ran = append(ran, id)
		return nil
	}}
	rs := whenSwarm("", 100)

	if err := o.runLevels(NewRun("probe"), rs, [][]string{{"gate"}, {"notify"}}); err != nil {
		t.Fatalf("the run failed: %v", err)
	}
	if len(ran) != 2 {
		t.Errorf("ran %v, want both — no when: means always run", ran)
	}
}

// A fatal failure alongside a tolerated one in the same wave still fails
// the run — "continue" is per bot, not a mood the whole wave catches.
func TestOneToleratedFailureDoesNotExcuseAFatalOne(t *testing.T) {
	o := &Orchestrator{runBotFn: func(_ *Run, _ *planner.ResolvedSwarm, id string, _ *planner.ResolvedBot) error {
		return fmt.Errorf("boom in %s", id)
	}}
	run := NewRun("probe")
	rs := swarmWith([]schema.BotRef{
		{ID: "soft", OnError: schema.OnErrorContinue},
		{ID: "hard"},
	}, nil)

	err := o.runLevels(run, rs, [][]string{{"soft", "hard"}})
	if err == nil {
		t.Fatal("the run survived a fatal failure")
	}
	if strings.Contains(err.Error(), "soft") {
		t.Errorf("the tolerated bot was reported as fatal: %v", err)
	}
	if len(run.GetTolerated()) != 1 {
		t.Errorf("the tolerated failure was lost: %+v", run.GetTolerated())
	}
}

// A run made entirely of demo data succeeds, produces plausible emails and
// invoices, and is indistinguishable from a real one — and that is the
// *default*, since every bot ships on `connection: demo`. The most
// misleading thing this product can do is succeed convincingly on invented
// data without saying so.
func TestARunRecordsWhichServicesWereDemoData(t *testing.T) {
	run := NewRun("probe")
	run.NoteDemoService("triage", "gmail")
	run.NoteDemoService("triage", "gmail") // same fact twice is one fact
	run.NoteDemoService("mailer", "gdrive")

	got := run.DemoServices()
	if len(got) != 2 {
		t.Fatalf("got %v, want two distinct pairs", got)
	}
	// Sorted, so a run detail page renders the same way twice.
	if got[0] != "mailer.gdrive" || got[1] != "triage.gmail" {
		t.Errorf("got %v, want sorted bot.service pairs", got)
	}

	// A run that touched nothing demo says nothing, rather than an empty
	// banner on every live run.
	if len(NewRun("clean").DemoServices()) != 0 {
		t.Error("a clean run reported demo services")
	}
}

// fakeApprovalAPI stands in for 1Claw's approval endpoints, so the race
// between the local queue and the phone can be tested without a network.
//
// It enforces the rule the real API enforces: /v1/approvals/request is
// agent-only, and a bearer minted from the *human* exchange gets 403 "Only
// agents can request approvals." The previous version of this fake accepted
// any token, which is how a mirror that could never have worked against the
// real 1Claw kept a green test for the life of the repo.
func fakeApprovalAPI(t *testing.T, statusAfter func(polls int) string) *oneclaw.Client {
	t.Helper()
	polls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/auth/agent-token"):
			// What an ocv_ key exchanges at. The token says which door it
			// came through, so the handler below can tell them apart.
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "agent-tok", "token_type": "Bearer", "expires_in": 86400,
			})
		case strings.HasSuffix(r.URL.Path, "/auth/api-key-token"):
			// The human exchange. Succeeds — it is the approvals endpoint
			// that refuses this credential, not the exchange.
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "human-tok", "token_type": "Bearer", "expires_in": 86400,
			})
		case strings.HasSuffix(r.URL.Path, "/approvals/request"):
			if r.Header.Get("Authorization") != "Bearer agent-tok" {
				w.WriteHeader(http.StatusForbidden)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"detail": "Only agents can request approvals.",
				})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "remote-1", "status": "pending"})
		case strings.Contains(r.URL.Path, "/approvals/") && strings.HasSuffix(r.URL.Path, "/status"):
			polls++
			_ = json.NewEncoder(w).Encode(map[string]any{"status": statusAfter(polls)})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	orig := oneclaw.DefaultBaseURL
	oneclaw.DefaultBaseURL = srv.URL
	t.Cleanup(func() { oneclaw.DefaultBaseURL = orig })
	return oneclaw.NewAgentClient("ocv_test-key")
}

// mirrorTo is what Orchestrator.approvalAgent gives a real approver: an
// agent-authenticated client and the agent's id.
func mirrorTo(client *oneclaw.Client) ApprovalMirror {
	return func() (*oneclaw.Client, string, error) { return client, "agent-1", nil }
}

// An overnight swarm should be answerable from a phone, not only from a
// browser tab that happens to be open. The same question goes to both
// queues and the first answer wins.
func TestAnApprovalAnsweredOnAPhoneDecidesTheRun(t *testing.T) {
	// A real poll interval would make this test a five-second wait for
	// nothing; the interval is not what is under test.
	orig := mirrorPoll
	mirrorPoll = 10 * time.Millisecond
	t.Cleanup(func() { mirrorPoll = orig })

	run := NewRun("probe")
	a := &RunQueueApprover{
		Run: run, Bot: "sender", Step: "approve",
		Mirror: mirrorTo(fakeApprovalAPI(t, func(int) string { return "approved" })),
	}

	type result struct {
		ok bool
		by string
	}
	res := make(chan result, 1)
	go func() {
		ok, by, _ := a.Approve("Send 20 reminders", "high")
		res <- result{ok, by}
	}()

	select {
	case r := <-res:
		if !r.ok {
			t.Error("a remote approval did not approve the run")
		}
		// Who decided has to survive: "approved by someone on their phone"
		// is different from "approved in this tab".
		if !strings.Contains(r.by, "1claw") {
			t.Errorf("decided_by = %q, want it to name 1Claw", r.by)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the run never learned about the remote decision")
	}
}

// The local queue is the one that must work. A 1Claw outage should cost you
// the convenience of approving from your phone, not the ability to approve.
func TestAFailingMirrorDoesNotBlockLocalApproval(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	orig := oneclaw.DefaultBaseURL
	oneclaw.DefaultBaseURL = srv.URL
	defer func() { oneclaw.DefaultBaseURL = orig }()

	run := NewRun("probe")
	a := &RunQueueApprover{
		Run: run, Bot: "sender", Step: "approve",
		Mirror: mirrorTo(oneclaw.NewAgentClient("ocv_k")),
	}

	done := make(chan bool, 1)
	go func() {
		ok, _, _ := a.Approve("Send it", "medium")
		done <- ok
	}()

	// The local gate must still open and still be answerable.
	deadline := time.After(5 * time.Second)
	for {
		if p := run.PendingApprovals(); len(p) > 0 {
			_ = run.Decide(p[0].ID, true, "cli")
			break
		}
		select {
		case <-deadline:
			t.Fatal("no local approval was ever queued")
		case <-time.After(5 * time.Millisecond):
		}
	}
	select {
	case ok := <-done:
		if !ok {
			t.Error("the local decision did not take")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Approve never returned")
	}
	// And it said so, rather than failing silently.
	var warned bool
	for _, l := range run.LogEntries() {
		if strings.Contains(l.Msg, "answerable here only") {
			warned = true
		}
	}
	if !warned {
		t.Errorf("nothing said the mirror failed: %+v", run.LogEntries())
	}
}

// No 1Claw is the ordinary local-only case and must not reach the network
// or change behaviour at all.
func TestWithoutOneClawApprovalIsLocalOnly(t *testing.T) {
	run := NewRun("probe")
	a := &RunQueueApprover{Run: run, Bot: "b", Step: "approve"}
	go func() {
		deadline := time.After(3 * time.Second)
		for {
			if p := run.PendingApprovals(); len(p) > 0 {
				_ = run.Decide(p[0].ID, true, "cli")
				return
			}
			select {
			case <-deadline:
				return
			case <-time.After(time.Millisecond):
			}
		}
	}()
	ok, by, err := a.Approve("x", "low")
	if err != nil || !ok || by != "cli" {
		t.Errorf("ok=%v by=%q err=%v", ok, by, err)
	}
	for _, l := range run.LogEntries() {
		if strings.Contains(l.Msg, "1Claw") {
			t.Errorf("mentioned 1Claw with none configured: %q", l.Msg)
		}
	}
}

// A transient failure — a flaky service call, a container that lost its
// network for a second — should not need a human to press Run again.
func TestABotCanBeRetried(t *testing.T) {
	var attempts int
	o := &Orchestrator{runBotFn: func(_ *Run, _ *planner.ResolvedSwarm, _ string, _ *planner.ResolvedBot) error {
		attempts++
		if attempts < 3 {
			return fmt.Errorf("connection reset")
		}
		return nil
	}}
	rs := swarmWith([]schema.BotRef{{ID: "flaky", Retry: 3}}, nil)

	run := NewRun("probe")
	if err := o.runLevels(run, rs, [][]string{{"flaky"}}); err != nil {
		t.Fatalf("gave up on a retryable failure: %v", err)
	}
	if attempts != 3 {
		t.Errorf("attempts = %d, want 3", attempts)
	}
	// Each retry is visible: a bot quietly succeeding on its third go is
	// something worth knowing about the service behind it.
	var said int
	for _, l := range run.LogEntries() {
		if strings.Contains(l.Msg, "retrying") {
			said++
		}
	}
	if said != 2 {
		t.Errorf("%d retry lines, want 2", said)
	}
}

// The wait is real work, not a log line: this asserts the orchestrator
// actually asked to sleep the declared duration, through the same seam a
// real deploy sleeps through, without the test itself taking any time.
func TestARetryWaitsItsDeclaredBackoff(t *testing.T) {
	var attempts int
	o := &Orchestrator{
		runBotFn: func(_ *Run, _ *planner.ResolvedSwarm, _ string, _ *planner.ResolvedBot) error {
			attempts++
			if attempts < 2 {
				return fmt.Errorf("connection reset")
			}
			return nil
		},
	}
	var waited []time.Duration
	o.retryWaitFn = func(_ context.Context, d time.Duration) { waited = append(waited, d) }
	rs := swarmWith([]schema.BotRef{{ID: "flaky", Retry: 2, RetryBackoff: "5s"}}, nil)

	run := NewRun("probe")
	if err := o.runLevels(run, rs, [][]string{{"flaky"}}); err != nil {
		t.Fatalf("gave up on a retryable failure: %v", err)
	}
	if len(waited) != 1 || waited[0] != 5*time.Second {
		t.Errorf("waited %v, want one 5s wait", waited)
	}
}

func TestNoRetryByDefault(t *testing.T) {
	var attempts int
	o := &Orchestrator{runBotFn: func(_ *Run, _ *planner.ResolvedSwarm, _ string, _ *planner.ResolvedBot) error {
		attempts++
		return fmt.Errorf("boom")
	}}
	if err := o.runLevels(NewRun("probe"), swarmWith([]schema.BotRef{{ID: "b"}}, nil),
		[][]string{{"b"}}); err == nil {
		t.Fatal("expected a failure")
	}
	if attempts != 1 {
		t.Errorf("attempts = %d, want 1 — retry must be opt-in", attempts)
	}
}

// Asking again until someone says yes is not a retry, it is wearing them
// down. A declined approval is a decision and must stand.
func TestADeclinedApprovalIsNeverRetried(t *testing.T) {
	var attempts int
	o := &Orchestrator{runBotFn: func(_ *Run, _ *planner.ResolvedSwarm, _ string, _ *planner.ResolvedBot) error {
		attempts++
		return fmt.Errorf(`step "gate": not approved (decided_by=cli)`)
	}}
	if err := o.runLevels(NewRun("probe"), swarmWith([]schema.BotRef{{ID: "sender", Retry: 3}}, nil),
		[][]string{{"sender"}}); err == nil {
		t.Fatal("expected a failure")
	}
	if attempts != 1 {
		t.Errorf("a declined approval was asked %d times", attempts)
	}
}
