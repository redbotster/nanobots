package api

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// The stampede this type exists to prevent.
//
// A plain get/put cache made the cold case worse than no cache at all:
// measured against the live account, four concurrent requests to a cold
// /api/connections took 15.7s each, where a single uncached request took
// 7.8s. Each request fans out to eight throttled vault reads, so four of
// them queued thirty-two reads and everyone waited for the pile.
func TestConcurrentCallersShareOneComputation(t *testing.T) {
	var c ttlCache[int]
	var calls atomic.Int64
	release := make(chan struct{})

	var wg sync.WaitGroup
	results := make([]int, 8)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, err := c.do(time.Minute, func() (int, error) {
				calls.Add(1)
				<-release // hold the flight open so the others pile up behind it
				return 42, nil
			})
			if err != nil {
				t.Error(err)
			}
			results[i] = v
		}()
	}
	// Give the eight goroutines time to arrive before the first one finishes.
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()

	if got := calls.Load(); got != 1 {
		t.Errorf("the work ran %d times for 8 concurrent callers, want 1", got)
	}
	for i, v := range results {
		if v != 42 {
			t.Errorf("caller %d got %d, want the shared answer 42", i, v)
		}
	}
}

func TestTheAnswerIsServedFromCacheUntilItExpires(t *testing.T) {
	var c ttlCache[int]
	var calls atomic.Int64
	compute := func() (int, error) { calls.Add(1); return int(calls.Load()), nil }

	if v, _ := c.do(time.Minute, compute); v != 1 {
		t.Fatalf("first = %d", v)
	}
	if v, _ := c.do(time.Minute, compute); v != 1 {
		t.Errorf("second = %d, want the cached 1", v)
	}
	// A zero TTL makes everything already stale.
	if v, _ := c.do(0, compute); v != 2 {
		t.Errorf("after expiry = %d, want a fresh 2", v)
	}
}

func TestAFailureIsNotRemembered(t *testing.T) {
	var c ttlCache[string]
	boom := errors.New("1claw is down")

	v, err := c.do(time.Minute, func() (string, error) { return "partial", boom })
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	if v != "partial" {
		t.Errorf("value = %q — a caller with something useful to say about a "+
			"failure still needs the value back", v)
	}
	// The next call must actually retry rather than serve the failure.
	got, err := c.do(time.Minute, func() (string, error) { return "recovered", nil })
	if err != nil || got != "recovered" {
		t.Errorf("got (%q, %v), want a real retry", got, err)
	}
}

// The subtle one. A read that starts before the user connects an account
// finishes after it, and without the generation check it would store what
// it saw beforehand — overwriting the invalidation and making the app go on
// reporting "not connected" about something it connected itself.
func TestAnInvalidationDuringAFlightIsNotLost(t *testing.T) {
	var c ttlCache[string]
	inFlight := make(chan struct{})
	release := make(chan struct{})

	var stale string
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		stale, _ = c.do(time.Minute, func() (string, error) {
			close(inFlight)
			<-release
			return "before the write", nil
		})
	}()

	<-inFlight
	c.invalidate() // the user connects something while the read is in progress
	close(release)
	wg.Wait()

	if stale != "before the write" {
		t.Fatalf("the in-flight caller should still get its own answer, got %q", stale)
	}

	// The next caller must not be served the overtaken value.
	fresh, _ := c.do(time.Minute, func() (string, error) { return "after the write", nil })
	if fresh != "after the write" {
		t.Errorf("got %q — the flight stored a result that its invalidation had "+
			"already overtaken, so the cache is lying about a change the app made", fresh)
	}
}

func TestInvalidateForcesTheNextReadToRecompute(t *testing.T) {
	var c ttlCache[int]
	var calls atomic.Int64
	compute := func() (int, error) { calls.Add(1); return int(calls.Load()), nil }

	c.do(time.Minute, compute)
	c.do(time.Minute, compute)
	if calls.Load() != 1 {
		t.Fatalf("calls = %d before invalidating", calls.Load())
	}
	c.invalidate()
	if v, _ := c.do(time.Minute, compute); v != 2 {
		t.Errorf("after invalidate = %d, want a recomputed 2", v)
	}
}

// An expired value is served immediately and corrected behind the request.
//
// The blocking version made the TTL something a person waits for:
// /api/connections is 3.5s cold, so with a one-minute TTL whichever page
// load first crossed the minute paid 3.5s again — on an endpoint four
// screens fetch on mount, one of them the landing page's own.
func TestAnExpiredValueIsServedWhileItIsRefreshed(t *testing.T) {
	var c ttlCache[string]
	var calls atomic.Int32
	slow := func() (string, error) {
		calls.Add(1)
		time.Sleep(60 * time.Millisecond)
		return "fresh", nil
	}

	if v, _ := c.doStale(time.Minute, func() (string, error) { return "first", nil }); v != "first" {
		t.Fatalf("cold read = %q", v)
	}

	// Expire it, then read with a computation slow enough that a blocking
	// implementation could not possibly return in time.
	c.mu.Lock()
	c.at = time.Now().Add(-time.Hour)
	c.mu.Unlock()

	start := time.Now()
	v, err := c.doStale(time.Minute, slow)
	if err != nil {
		t.Fatal(err)
	}
	if v != "first" {
		t.Errorf("got %q — an expired read waited for the new value instead of serving the held one", v)
	}
	if took := time.Since(start); took > 30*time.Millisecond {
		t.Errorf("an expired read took %s, so the caller waited for the refresh", took)
	}

	// And the refresh really happened, so the next reader gets the new one.
	waitFor(t, func() bool { return calls.Load() == 1 })
	waitFor(t, func() bool {
		v, _ := c.doStale(time.Minute, slow)
		return v == "fresh"
	})
}

// A cold cache blocks, and must. There is nothing to be stale with, and
// answering "nothing is connected" because the answer has not arrived would
// be the app claiming something untrue about a real account.
func TestAColdReadStillWaitsForARealAnswer(t *testing.T) {
	var c ttlCache[string]
	v, err := c.doStale(time.Minute, func() (string, error) {
		time.Sleep(20 * time.Millisecond)
		return "real", nil
	})
	if err != nil || v != "real" {
		t.Errorf("cold doStale returned (%q, %v) — it did not wait for the answer", v, err)
	}
}

// The distinction the whole design rests on. invalidate() is what this app
// calls when it has just connected an account, and that reader is watching
// for the change — so it zeroes the value and the next read blocks for a
// fresh one. Only TTL expiry gets the stale treatment.
func TestAnInvalidatedValueIsNotServedStale(t *testing.T) {
	var c ttlCache[string]
	if _, err := c.doStale(time.Minute, func() (string, error) { return "before", nil }); err != nil {
		t.Fatal(err)
	}
	c.invalidate()

	v, err := c.doStale(time.Minute, func() (string, error) { return "after", nil })
	if err != nil {
		t.Fatal(err)
	}
	if v != "after" {
		t.Errorf("got %q — a read after the app changed something served the answer from before it", v)
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	for i := 0; i < 200; i++ {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition never became true")
}

// The bug the first version of doStale had, which only a stopwatch found.
//
// It served stale only when no flight was in progress, so the *second*
// request to arrive during a refresh fell through to do() and waited on it.
// Against the live daemon that read 0.97ms, then 3.29s, then 2ms — the
// three-second wait had moved rather than gone, and it landed on whoever
// loaded a page a moment after someone else.
func TestEveryReaderDuringARefreshIsServedImmediately(t *testing.T) {
	var c ttlCache[string]
	var calls atomic.Int32
	release := make(chan struct{})
	slow := func() (string, error) {
		calls.Add(1)
		<-release // held open for the whole test
		return "fresh", nil
	}

	if _, err := c.doStale(time.Minute, func() (string, error) { return "held", nil }); err != nil {
		t.Fatal(err)
	}
	c.mu.Lock()
	c.at = time.Now().Add(-time.Hour)
	c.mu.Unlock()

	// The first read starts the refresh; the next three arrive while it is
	// still running and must not wait for it.
	for i := 0; i < 4; i++ {
		start := time.Now()
		v, err := c.doStale(time.Minute, slow)
		if err != nil {
			t.Fatal(err)
		}
		if v != "held" {
			t.Errorf("read %d returned %q, want the held value", i, v)
		}
		if took := time.Since(start); took > 50*time.Millisecond {
			t.Fatalf("read %d waited %s for a refresh that is still running", i, took)
		}
	}

	close(release)
	// And exactly one refresh ran for all four, which is the other half of
	// the promise: this must not turn into a call per reader.
	waitFor(t, func() bool { return calls.Load() == 1 })
	if n := calls.Load(); n != 1 {
		t.Errorf("%d refreshes ran, want 1", n)
	}
}
