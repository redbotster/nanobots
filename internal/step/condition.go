package step

import (
	"fmt"
	"strconv"
	"strings"
)

// conditionOps is checked in this order because "<=" contains "<" — matching
// the shorter operator first would cut "<=" into "<" plus a stray "=" and
// silently misparse every bounded condition in the catalog.
var conditionOps = []string{"==", "!=", "<=", ">=", "<", ">"}

// ParsedCondition is a when: expression split at its comparison operator.
// Op is "" for a bare truthy check ("{{inputs.urgent}}" with no comparison
// at all), in which case Right is unused.
type ParsedCondition struct {
	Left  string
	Op    string
	Right string
}

// ParseCondition splits a when: string at its comparison operator, if any.
// internal/planner uses this to validate the expression at plan time (every
// {{...}} in Left and Right has to be a real input port); internal/runner
// uses it again, unchanged, to evaluate the same expression at run time —
// one parse, so the two cannot disagree about what an expression means.
func ParseCondition(expr string) (ParsedCondition, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return ParsedCondition{}, fmt.Errorf("empty condition")
	}
	for _, op := range conditionOps {
		idx := strings.Index(expr, op)
		if idx < 0 {
			continue
		}
		left := strings.TrimSpace(expr[:idx])
		right := strings.TrimSpace(expr[idx+len(op):])
		if left == "" || right == "" {
			return ParsedCondition{}, fmt.Errorf("%q: %s needs a value on both sides", expr, op)
		}
		return ParsedCondition{Left: left, Op: op, Right: right}, nil
	}
	return ParsedCondition{Left: expr}, nil
}

// TemplatePaths returns every {{...}} path referenced in s, stripped of any
// "| default: ..." filter — just the dotted path a plan-time check needs to
// confirm points somewhere real. Exported for the same reason ListIndex is:
// internal/planner walks the same syntax this package resolves, and the two
// must use the identical rule for what counts as a reference.
func TemplatePaths(s string) []string {
	var paths []string
	for _, m := range templateExpr.FindAllStringSubmatch(s, -1) {
		inner := strings.TrimSpace(m[1])
		path, _, _ := strings.Cut(inner, "|")
		paths = append(paths, strings.TrimSpace(path))
	}
	return paths
}

// EvalCondition resolves and evaluates a when: expression against ctx (the
// same {{...}} resolution every other template goes through). A bare
// expression (no comparison) is a truthy check, using the same "empty
// counts as absent" rule as everything else — see isEmptyValue.
func EvalCondition(expr string, ctx map[string]any) (bool, error) {
	parsed, err := ParseCondition(expr)
	if err != nil {
		return false, err
	}
	left := resolveValue(parsed.Left, ctx)
	if parsed.Op == "" {
		return truthy(left), nil
	}
	right := resolveValue(parsed.Right, ctx)
	lf, lok := toFloat(left)
	rf, rok := toFloat(right)
	if lok && rok {
		switch parsed.Op {
		case "==":
			return lf == rf, nil
		case "!=":
			return lf != rf, nil
		case "<":
			return lf < rf, nil
		case "<=":
			return lf <= rf, nil
		case ">":
			return lf > rf, nil
		case ">=":
			return lf >= rf, nil
		}
	}
	// Neither side is a number: only equality means anything for text ("is
	// this the same status as last time"), the same restriction stop.if
	// already applies. Ordering two strings would silently answer a
	// question nobody asked.
	switch parsed.Op {
	case "==":
		return fmt.Sprint(left) == fmt.Sprint(right), nil
	case "!=":
		return fmt.Sprint(left) != fmt.Sprint(right), nil
	default:
		return false, fmt.Errorf("%q: %s compares %v to %v, and neither is a number",
			expr, parsed.Op, left, right)
	}
}

// truthy is what a bare condition (no comparison) tests: the same "empty
// counts as absent" rule as a template's own default filter, plus the
// obvious string and number spellings of false.
func truthy(v any) bool {
	if isEmptyValue(v) {
		return false
	}
	switch t := v.(type) {
	case bool:
		return t
	case string:
		return t != "false" && t != "0"
	case float64:
		return t != 0
	}
	return true
}

// toFloat reports whether v is (or looks exactly like) a number, so a
// condition can compare "500" to 500 the way a human reading a swarm file
// would expect a threshold on a templated port to work.
func toFloat(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case int:
		return float64(t), true
	case string:
		f, err := strconv.ParseFloat(t, 64)
		return f, err == nil
	}
	return 0, false
}
