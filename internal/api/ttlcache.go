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
}

// get returns the cached value if it was stored within ttl.
//
// The value is returned as stored, not copied. That is safe for the way both
// callers use it — put is always handed a freshly built value and nothing
// ever mutates one in place afterwards, it only gets replaced — and a deep
// copy is not something this type can do generically. A caller that wants to
// mutate what it gets back must copy it first.
func (c *ttlCache[T]) get(ttl time.Duration) (T, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.set || time.Since(c.at) > ttl {
		var zero T
		return zero, false
	}
	return c.val, true
}

func (c *ttlCache[T]) put(v T) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.val, c.at, c.set = v, time.Now(), true
}

// invalidate drops the cached value so the next get re-reads.
//
// This is what keeps a cache from lying. Where this app itself performs the
// change — connecting an account writes a vault secret — the handler that
// made the change calls this, so the next read reflects it immediately
// rather than after the TTL. The TTL is only the backstop for changes made
// somewhere this app cannot see.
func (c *ttlCache[T]) invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	var zero T
	c.val, c.at, c.set = zero, time.Time{}, false
}
