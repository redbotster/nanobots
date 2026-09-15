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
