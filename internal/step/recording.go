package step

import (
	"fmt"
	"sync"

	"github.com/redbotster/nanobots/internal/schema"
)

// RecordingDeps watches a real run go past and keeps what a fixture would
// have to contain.
//
// Every bot in this repo ships fixtures so `nanobots conform` and `go test`
// can run it offline, and those fixtures are hand-written — which means
// they are somebody's guess at what a model or an API returns. Guesses
// drift: a fixture written before a bot's prompt changed still passes,
// while the real bot has been broken for a month. Four fixtures were
// hand-written in a single day of work on this repo, and one of them
// (support-triage's drafts.create) was wrong in a way that only a real run
// exposed.
//
// So this captures the real thing. It wraps whatever Deps a bot actually
// ran against and records exactly the values DemoDeps would later read
// back, under exactly the filenames it reads them from — the names come
// from one place (fixtureName) used by both, so a recorded fixture cannot
// be filed somewhere conformance won't look.
//
// It records only what a fixture holds: the model's response, service call
// results, a recall answer, a fetch. Approvals, memory writes and
// notifications are decisions and side effects, not test data.
//
// Wrapping rather than hooking each backend keeps this out of the live
// path's way: nothing here can change what a bot sees, because every method
// returns the inner value untouched and records afterwards. A failure is
// still recorded — a fixture of the error case is often the one worth
// having — but an error is never turned into a value.
type RecordingDeps struct {
	Inner Deps

	mu       sync.Mutex
	captured map[string]any
	// order preserves first-seen order so a diff of two recordings reads
	// the same way twice.
	order []string
}

func NewRecordingDeps(inner Deps) *RecordingDeps {
	return &RecordingDeps{Inner: inner, captured: map[string]any{}}
}

// fixtureName is the single source of truth for what a captured value is
// called. DemoDeps reads these exact names; see its loadFixture calls.
func fixtureName(kind, a, b string) string {
	switch kind {
	case "service":
		return fmt.Sprintf("%s.%s.json", a, b)
	case "ai":
		return "ai.generate.json"
	case "web":
		return "web.fetch.json"
	case "recall":
		return "memory.recall.json"
	}
	return ""
}

func (r *RecordingDeps) record(name string, v any) {
	if name == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, seen := r.captured[name]; !seen {
		r.order = append(r.order, name)
	}
	// Last write wins: a bot that calls the same op twice in one run would
	// otherwise pin whichever happened first, and the later call is the
	// one whose shape the bot most recently depended on.
	r.captured[name] = v
}

// Captured returns the fixtures this run would produce, in first-seen
// order, as filename -> value.
func (r *RecordingDeps) Captured() map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make(map[string]any, len(r.captured))
	for k, v := range r.captured {
		out[k] = v
	}
	return out
}

// CapturedNames returns the filenames in first-seen order.
func (r *RecordingDeps) CapturedNames() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.order))
	copy(out, r.order)
	return out
}

func (r *RecordingDeps) ServiceCall(svc schema.Service, op string, params map[string]any) (any, error) {
	out, err := r.Inner.ServiceCall(svc, op, params)
	if err == nil {
		r.record(fixtureName("service", svc.ID, op), out)
	}
	return out, err
}

// AIGenerate records the parsed response rather than the raw string.
//
// A fixture is read back with json.Unmarshal, so what belongs on disk is
// the JSON the model produced — not a JSON string containing it, and not a
// fenced block. extractJSON is the same function the interpreter uses to
// read the response, so a recorded fixture is exactly what the bot acted
// on, including the rescue of a chatty answer.
func (r *RecordingDeps) AIGenerate(prompt string, model schema.Model) (string, error) {
	out, err := r.Inner.AIGenerate(prompt, model)
	if err == nil {
		if parsed, ok := parseJSONValue(extractJSON(out)); ok {
			r.record(fixtureName("ai", "", ""), parsed)
		}
	}
	return out, err
}

func (r *RecordingDeps) WebFetch(params map[string]any) (any, error) {
	out, err := r.Inner.WebFetch(params)
	if err == nil {
		r.record(fixtureName("web", "", ""), out)
	}
	return out, err
}

// MemoryRecall records the answer keyed by the question, which is the
// map form DemoDeps supports — a bot asking two questions in one run then
// replays both, where a bare string would replay one for everything.
func (r *RecordingDeps) MemoryRecall(namespace, question string) (string, error) {
	out, err := r.Inner.MemoryRecall(namespace, question)
	if err == nil && out != "" {
		name := fixtureName("recall", "", "")
		r.mu.Lock()
		existing, _ := r.captured[name].(map[string]string)
		if existing == nil {
			existing = map[string]string{}
		}
		existing[question] = out
		r.mu.Unlock()
		r.record(name, existing)
	}
	return out, err
}

// Everything below is pass-through: a decision or a side effect, not test
// data. Approvals in particular must never become a fixture — a recorded
// "approved" would turn a gate into a rubber stamp on every later run.
func (r *RecordingDeps) Render(t string, d any, to string) ([]byte, string, error) {
	return r.Inner.Render(t, d, to)
}
func (r *RecordingDeps) Now() string { return r.Inner.Now() }
func (r *RecordingDeps) MemoryGet(ns, k string) (string, bool, error) {
	return r.Inner.MemoryGet(ns, k)
}
func (r *RecordingDeps) MemoryPut(ns, k, v string) error { return r.Inner.MemoryPut(ns, k, v) }
func (r *RecordingDeps) MemoryRemember(ns, t string) error {
	return r.Inner.MemoryRemember(ns, t)
}
func (r *RecordingDeps) Approve(summary, tier string) (bool, string, error) {
	return r.Inner.Approve(summary, tier)
}
func (r *RecordingDeps) Notify(m, c string) error { return r.Inner.Notify(m, c) }
func (r *RecordingDeps) Blobs() BlobStore         { return r.Inner.Blobs() }

var _ Deps = (*RecordingDeps)(nil)
