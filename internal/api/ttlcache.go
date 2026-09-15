package api

import (
	"sync"
	"time"
)

// A small hold-the-last-answer cache, for endpoints whose answer costs
// several 1Claw round trips.
//
// Two endpoints need this and the reason is the same for both: 1Claw is
// roughly 900ms away and throttles concurrent reads, so an answer assembled
// from several calls costs seconds no matter what this end does.
// /api/connections reads eight vault secrets; /api/posture reads a summary,
// a quota and a topology. Both are loaded by pages a human opens repeatedly,
// and neither changes often.
//
// Generic rather than two hand-rolled structs so the expiry rule lives in
// one place — the second copy is where the two would quietly drift apart.
type ttlCache[T any] struct {
	mu  sync.Mutex
	val T
	at  time.Time
	set bool
	// gen counts invalidations. A flight compares the generation it started
	// in against the one current when it finishes, and declines to store a
	// result that was already overtaken — see do().
	gen    uint64
	flight *flight[T]
}

// flight is one in-progress computation that later arrivals wait on rather
// than duplicating.
type flight[T any] struct {
	done chan struct{}
	val  T
	err  error
}

// do returns the cached value, or computes it with fn — once, however many
// callers arrive at the same moment.
//
// The single-flight half is not a refinement, it is the point. A plain
// get/put cache made the cold case *worse* than no cache at all, measured
// against the live account: four concurrent requests to a cold
// /api/connections took 15.7 seconds each, against 7.8s for one request
// with no cache present. Each request fans out to eight vault reads, so
// four of them put thirty-two concurrent reads into a service that
// throttles, and every one of them waited for the whole pile.
//
// Four concurrent requests is not a contrived load, either: the app fires
// them itself. Watching a real page load in a browser showed /api/status
// requested twice at once, both paying full price, which is what sent me
// looking for this.
//
// fn returning an error means "do not remember this". The value is still
// handed back, so a caller that has something useful to say about a failure
// can say it; it just will not be served to anyone else.
func (c *ttlCache[T]) do(ttl time.Duration, fn func() (T, error)) (T, error) {
	c.mu.Lock()
	if c.set && time.Since(c.at) <= ttl {
		v := c.val
		c.mu.Unlock()
		return v, nil
	}
	if f := c.flight; f != nil {
		// Someone else is already asking. Wait for their answer instead of
		// making the same calls alongside them.
		c.mu.Unlock()
		<-f.done
		return f.val, f.err
	}
	f := &flight[T]{done: make(chan struct{})}
	c.flight = f
	startedAt := c.gen
	c.mu.Unlock()

	f.val, f.err = fn()

	c.mu.Lock()
	// Only store if nothing invalidated while this was in flight. Otherwise
	// a read that started before the user connected an account would finish
	// after, overwrite the invalidation with what it saw beforehand, and go
	// on reporting "not connected" for the rest of the TTL — the cache
	// lying about something the app itself did.
	if f.err == nil && c.gen == startedAt {
		c.val, c.at, c.set = f.val, time.Now(), true
	}
	c.flight = nil
	c.mu.Unlock()
	// After the store, so a waiter that wakes and re-reads sees a warm
	// cache rather than racing the next request into another flight.
	close(f.done)

	return f.val, f.err
}

// invalidate drops the cached value so the next do() recomputes.
//
// This is what keeps a cache from lying. Where this app itself performs the
// change — connecting an account writes a vault secret — the handler that
// made the change calls this, so the next read reflects it immediately
// rather than after the TTL. The TTL is only the backstop for changes made
// somewhere this app cannot see.
//
// An in-flight computation is left running — someone is already waiting on
// it — but bumping gen means its result will not be stored, because it may
// have read the vault before the write this invalidation is announcing.
func (c *ttlCache[T]) invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	var zero T
	c.val, c.at, c.set = zero, time.Time{}, false
	c.gen++
}
