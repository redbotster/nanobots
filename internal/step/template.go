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
// path, optionally followed by `| default: <literal>` (the blueprint's
// fallback filter, e.g. "memory.last_run_at | default: now-24h"). The
// fallback is returned as a bare string; it's a literal, not itself a path.
func resolveExpr(inner string, ctx map[string]any) (any, bool) {
	path, fallback, hasFallback := strings.Cut(inner, "|")
	val, ok := lookupPath(ctx, strings.TrimSpace(path))
	if ok {
		return val, true
	}
	if hasFallback {
		fb := strings.TrimSpace(fallback)
		fb = strings.TrimPrefix(fb, "default:")
		return strings.TrimSpace(fb), true
	}
	return nil, false
}

// lookupPath walks a dotted path ("inputs.since", "steps.fetch.output.name")
// through nested map[string]any values in ctx.
func lookupPath(ctx map[string]any, path string) (any, bool) {
	segs := strings.Split(path, ".")
	var cur any = ctx
	for _, seg := range segs {
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
