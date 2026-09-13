// Package step is the universal step interpreter: it executes a Nanobot's
// spec.steps against resolved inputs and a Deps implementation (real 1Claw +
// demo fixtures, see deps.go), producing the bot's declared output ports.
// This is what both the `bare` and `openclaw` harnesses run in this build —
// see docs/harnesses.md for why there's no dynamic agent loop yet.
package step

import (
	"fmt"
	"regexp"
	"strings"
)

var templateExpr = regexp.MustCompile(`\{\{\s*([^}]+?)\s*\}\}`)

// ResolveTemplateValue is the exported form of resolveValue, for callers
// outside this package that need the same `{{...}}` resolution — currently
// internal/runner, for defaulting a swarm bot's inputs before a run starts.
func ResolveTemplateValue(v any, ctx map[string]any) any { return resolveValue(v, ctx) }

// resolveValue recursively resolves `{{...}}` template expressions in v
// against ctx. A string that is *entirely* one expression resolves to the
// looked-up value's native type (so `"{{steps.fetch.output}}"` yields the
// actual array/map, not its stringification); a string with an expression
// embedded in surrounding text is resolved via string interpolation.
func resolveValue(v any, ctx map[string]any) any {
	switch vv := v.(type) {
	case string:
		return resolveString(vv, ctx)
	case map[string]any:
		out := make(map[string]any, len(vv))
		for k, val := range vv {
			out[k] = resolveValue(val, ctx)
		}
		return out
	case []any:
		out := make([]any, len(vv))
		for i, val := range vv {
			out[i] = resolveValue(val, ctx)
		}
		return out
	default:
		return v
	}
}

func resolveString(s string, ctx map[string]any) any {
	locs := templateExpr.FindAllStringSubmatchIndex(s, -1)
	if len(locs) == 1 && locs[0][0] == 0 && locs[0][1] == len(s) {
		inner := s[locs[0][2]:locs[0][3]]
		val, _ := resolveExpr(inner, ctx)
		return val
	}
	return templateExpr.ReplaceAllStringFunc(s, func(m string) string {
		inner := strings.TrimSpace(m[2 : len(m)-2])
		val, _ := resolveExpr(inner, ctx)
		if val == nil {
			return ""
		}
		return fmt.Sprint(val)
	})
}

// resolveExpr resolves one `{{...}}` expression's inner content — a dotted
// path, optionally followed by `| default: <fallback>` (the blueprint's
// fallback filter, e.g. "memory.last_run_at | default: now-24h").
//
// The fallback is tried as a path first and used as a literal string when
// it doesn't resolve. That generalisation earns its keep: a webhook-driven
// swarm wants "{{trigger.payload | default: vars.example_lead}}" so it runs
// by hand as well as when a webhook fires, and a literal cannot carry a
// JSON object. Existing fallbacks are unaffected — "now-24h" resolves to
// nothing and stays the string it always was.
func resolveExpr(inner string, ctx map[string]any) (any, bool) {
	path, fallback, hasFallback := strings.Cut(inner, "|")
	val, ok := lookupPath(ctx, strings.TrimSpace(path))
	if ok && !isEmptyValue(val) {
		return val, true
	}
	if hasFallback {
		fb := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(fallback), "default:"))
		if v, ok := lookupPath(ctx, fb); ok && !isEmptyValue(v) {
			return v, true
		}
		return fb, true
	}
	return val, ok
}

// isEmptyValue decides whether a resolved value should fall through to the
// fallback.
//
// A path that exists but holds nothing is the same situation as a path that
// doesn't exist: `trigger.payload` is present on every run and empty on the
// ones no webhook started. Treating "" and nil as absent is what makes one
// template work for both. A `false` or a `0` is a real value and stays.
func isEmptyValue(v any) bool {
	switch t := v.(type) {
	case nil:
		return true
	case string:
		return t == ""
	case map[string]any:
		return len(t) == 0
	case []any:
		return len(t) == 0
	}
	return false
}

// lookupPath walks a dotted path ("inputs.since", "steps.fetch.output.name")
// through nested map[string]any values in ctx. A purely-numeric segment
// ("draft_ids.0") indexes into a []any instead of a map key — the one way
// this build has to pull a single element out of a list<T> port (e.g. "the
// first draft's id" as its own string) rather than a numeric index scheme
// of its own; see internal/planner's matching support for type-checking it
// across a snap and internal/runner's for resolving the actual value.
func lookupPath(ctx map[string]any, path string) (any, bool) {
	segs := strings.Split(path, ".")
	var cur any = ctx
	for _, seg := range segs {
		if idx, isIndex := listIndex(seg); isIndex {
			arr, ok := cur.([]any)
			if !ok || idx < 0 || idx >= len(arr) {
				return nil, false
			}
			cur = arr[idx]
			continue
		}
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		cur, ok = m[seg]
		if !ok {
			return nil, false
		}
	}
	return cur, true
}

// ListIndex reports whether seg is a non-negative integer list index — the
// exported form, for internal/planner (type-checking a snap that indexes
// into a list<T> port) and internal/runner (resolving that snap's actual
// value at run time) to use the exact same rule this package does.
func ListIndex(seg string) (int, bool) { return listIndex(seg) }

// listIndex reports whether seg is a non-negative integer list index.
func listIndex(seg string) (int, bool) {
	if seg == "" {
		return 0, false
	}
	for _, r := range seg {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	n := 0
	for _, r := range seg {
		n = n*10 + int(r-'0')
	}
	return n, true
}
