package step

import (
	"reflect"
	"testing"
)

func TestResolveValue(t *testing.T) {
	ctx := map[string]any{
		"inputs": map[string]any{"since": "2026-09-09", "label": "INBOX"},
		"steps": map[string]any{
			"fetch": map[string]any{"output": []any{map[string]any{"id": "1"}}},
		},
		"memory": map[string]any{},
	}
	cases := []struct {
		name string
		in   any
		want any
	}{
		{"whole-string-expression-preserves-type", "{{steps.fetch.output}}", []any{map[string]any{"id": "1"}}},
		{"embedded-expression-interpolates", "after:{{inputs.since}} label:{{inputs.label}}", "after:2026-09-09 label:INBOX"},
		{"missing-key-with-fallback", "{{memory.last_run_at | default: now-24h}}", "now-24h"},
		{"missing-key-no-fallback-embedded", "x{{memory.nope}}y", "xy"},
		{"passthrough-non-string", 200, 200},
		{
			"nested-map",
			map[string]any{"q": "label:{{inputs.label}}", "max": 200},
			map[string]any{"q": "label:INBOX", "max": 200},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := resolveValue(c.in, ctx)
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("resolveValue(%#v) = %#v, want %#v", c.in, got, c.want)
			}
		})
	}
}

func TestLookupPath(t *testing.T) {
	ctx := map[string]any{"a": map[string]any{"b": map[string]any{"c": "leaf"}}}
	got, ok := lookupPath(ctx, "a.b.c")
	if !ok || got != "leaf" {
		t.Errorf("lookupPath(a.b.c) = %v, %v, want leaf, true", got, ok)
	}
	if _, ok := lookupPath(ctx, "a.b.missing"); ok {
		t.Error("expected missing path to report ok=false")
	}
}

// `default:` now falls back to another path when the fallback resolves,
// which is what lets one template serve a webhook run and a manual one:
// `{{trigger.payload | default: vars.example_lead}}`. A literal cannot
// carry a JSON object, so without this a webhook swarm is either
// unrunnable by hand or carries a fake lead in its inputs.
func TestADefaultCanFallBackToAnotherPath(t *testing.T) {
	ctx := map[string]any{
		"trigger": map[string]any{"payload": nil},
		"vars":    map[string]any{"example_lead": map[string]any{"name": "Dana"}},
	}
	got := ResolveTemplateValue("{{trigger.payload | default: vars.example_lead}}", ctx)
	m, ok := got.(map[string]any)
	if !ok || m["name"] != "Dana" {
		t.Fatalf("got %#v, want the example object", got)
	}

	// With a real payload, the payload wins.
	ctx["trigger"] = map[string]any{"payload": map[string]any{"name": "Priya"}}
	got = ResolveTemplateValue("{{trigger.payload | default: vars.example_lead}}", ctx)
	if m, _ := got.(map[string]any); m["name"] != "Priya" {
		t.Errorf("got %#v, want the delivered payload", got)
	}
}

// A fallback that isn't a path stays the literal string it always was —
// `memory.last_run_at | default: now-24h` predates this and must not break.
func TestALiteralFallbackStillWorks(t *testing.T) {
	ctx := map[string]any{"memory": map[string]any{}}
	if got := ResolveTemplateValue("{{memory.last_run_at | default: now-24h}}", ctx); got != "now-24h" {
		t.Errorf("got %#v, want the literal", got)
	}
}

// A path that exists but holds nothing is the same situation as one that
// doesn't: trigger.payload is present on every run and empty on the ones no
// webhook started.
func TestAnEmptyValueFallsThroughButFalseDoesNot(t *testing.T) {
	ctx := map[string]any{
		"a": map[string]any{"empty": "", "none": nil, "list": []any{}, "off": false, "zero": 0},
	}
	for _, path := range []string{"a.empty", "a.none", "a.list", "a.missing"} {
		if got := ResolveTemplateValue("{{"+path+" | default: fallback}}", ctx); got != "fallback" {
			t.Errorf("%s = %#v, want the fallback", path, got)
		}
	}
	// A false or a 0 is a real value someone chose, not an absence.
	if got := ResolveTemplateValue("{{a.off | default: fallback}}", ctx); got != false {
		t.Errorf("false fell through to the fallback: %#v", got)
	}
	if got := ResolveTemplateValue("{{a.zero | default: fallback}}", ctx); got != 0 {
		t.Errorf("zero fell through to the fallback: %#v", got)
	}
}
