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
