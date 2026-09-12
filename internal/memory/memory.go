// Package memory is what a bot remembers between runs.
//
// Until now there was one option and it was not really memory: a flat
// key/value store on a 1Claw agent, reachable only if 1Claw was configured
// at all. Two bots used it, for `last_run_at` and `last_summary`. That is a
// scratchpad — useful, but it cannot answer "what does this person care
// about", which is the thing a long-running assistant actually needs.
//
// So there are two interfaces here rather than one, because they are two
// genuinely different capabilities and pretending otherwise would force
// every backend to fake whichever half it doesn't have:
//
//	Store     durable key/value. Every backend has it. This is `last_run_at`.
//	Recaller  accumulate observations, ask questions in natural language.
//	          Only a backend that actually derives something can offer it.
//
// A backend advertises Recaller by implementing it; callers discover that
// with a type assertion and get a clear "not supported here" otherwise,
// rather than a silent empty answer. Composite lets the two halves come
// from different places — local key/value with Honcho recall, say — which
// is the arrangement most people will want.
package memory

import (
	"context"
	"errors"
	"fmt"
)

// Store is durable key/value memory, namespaced. Every backend provides it.
type Store interface {
	Get(ctx context.Context, namespace, key string) (value string, found bool, err error)
	Put(ctx context.Context, namespace, key, value string) error
}

// Recaller is memory that accumulates and reasons: record observations as
// they happen, then ask about them in plain language rather than by key.
//
// Optional on purpose. A key/value store cannot honestly implement Recall —
// it has nothing to reason over — and a backend that returned "" rather
// than saying so would make a bot silently behave as if it remembered
// nothing.
type Recaller interface {
	// Remember records one observation. Backends derive from these in the
	// background, so this returning nil means "accepted", not "processed".
	Remember(ctx context.Context, namespace, text string) error
	// Recall answers a question from what's been remembered. An empty
	// answer with a nil error means "nothing relevant", which is different
	// from an error.
	Recall(ctx context.Context, namespace, question string) (string, error)
}

// ErrNoRecall is returned when a bot asks for recall and the configured
// backend only does key/value. Named so callers can tell "this deployment
// can't do that" from "that went wrong".
var ErrNoRecall = errors.New("this memory backend stores keys and values but cannot answer questions — configure a recall-capable backend (see docs/memory.md)")

// RecallerOf returns s as a Recaller, or false if it only does key/value.
func RecallerOf(s Store) (Recaller, bool) {
	r, ok := s.(Recaller)
	return r, ok
}

// Recall is the one call sites should use: it asks s to answer question,
// and gives a clear error when the backend can't.
func Recall(ctx context.Context, s Store, namespace, question string) (string, error) {
	r, ok := RecallerOf(s)
	if !ok {
		return "", ErrNoRecall
	}
	return r.Recall(ctx, namespace, question)
}

// Remember records an observation when the backend can hold one, and is a
// no-op otherwise — a bot noting something down should not fail a run
// because this deployment only has key/value.
func Remember(ctx context.Context, s Store, namespace, text string) error {
	r, ok := RecallerOf(s)
	if !ok {
		return nil
	}
	return r.Remember(ctx, namespace, text)
}

// Composite takes key/value from one backend and recall from another, so a
// deployment can keep `last_run_at` on local disk while richer memory lives
// somewhere that can reason over it.
type Composite struct {
	KV   Store
	Rich Recaller // nil is fine: the composite is then key/value only
}

func (c *Composite) Get(ctx context.Context, ns, key string) (string, bool, error) {
	return c.KV.Get(ctx, ns, key)
}

func (c *Composite) Put(ctx context.Context, ns, key, value string) error {
	return c.KV.Put(ctx, ns, key, value)
}

func (c *Composite) Remember(ctx context.Context, ns, text string) error {
	if c.Rich == nil {
		return nil
	}
	return c.Rich.Remember(ctx, ns, text)
}

func (c *Composite) Recall(ctx context.Context, ns, question string) (string, error) {
	if c.Rich == nil {
		return "", ErrNoRecall
	}
	return c.Rich.Recall(ctx, ns, question)
}

// compile-time proof that Composite satisfies both halves.
var (
	_ Store    = (*Composite)(nil)
	_ Recaller = (*Composite)(nil)
)

func validKey(namespace, key string) error {
	if namespace == "" || key == "" {
		return fmt.Errorf("memory needs both a namespace and a key (got %q/%q)", namespace, key)
	}
	return nil
}

// DeferredOneClaw means "use 1Claw agent memory, once the agent is known".
//
// The backend is chosen at startup, but 1Claw memory is namespaced by agent
// id and a bot's agent only exists once the run reaches it. So startup
// leaves this marker and internal/runner swaps in a real OneClaw store per
// bot. Until then it behaves as Fallback rather than failing, so nothing
// depends on the order those two happen in — and a bot with no agent at all
// (most of them) simply keeps the fallback.
type DeferredOneClaw struct{ Fallback Store }

func (d *DeferredOneClaw) Get(ctx context.Context, ns, key string) (string, bool, error) {
	return d.Fallback.Get(ctx, ns, key)
}
func (d *DeferredOneClaw) Put(ctx context.Context, ns, key, value string) error {
	return d.Fallback.Put(ctx, ns, key, value)
}

// IsDeferredOneClaw reports whether s is waiting for an agent id.
func IsDeferredOneClaw(s Store) bool {
	_, ok := s.(*DeferredOneClaw)
	return ok
}

var _ Store = (*DeferredOneClaw)(nil)
