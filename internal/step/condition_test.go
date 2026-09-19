package step

import "testing"

func TestEvalCondition(t *testing.T) {
	ctx := map[string]any{
		"inputs": map[string]any{
			"amount": 750,
			"status": "overdue",
			"urgent": true,
			"quiet":  false,
			"empty":  "",
		},
	}
	cases := []struct {
		name string
		expr string
		want bool
	}{
		{"greater-than-true", "{{inputs.amount}} > 500", true},
		{"greater-than-false", "{{inputs.amount}} > 5000", false},
		{"less-than-or-equal", "{{inputs.amount}} <= 750", true},
		{"equals-string", `{{inputs.status}} == overdue`, true},
		{"not-equals-string", `{{inputs.status}} != overdue`, false},
		{"bare-truthy-true", "{{inputs.urgent}}", true},
		{"bare-truthy-false", "{{inputs.quiet}}", false},
		{"bare-truthy-empty-is-false", "{{inputs.empty}}", false},
		{"bare-truthy-missing-is-false", "{{inputs.nope}}", false},
		{"literal-vs-literal-numeric", "5 > 3", true},
		// A quoted literal is taken verbatim, empty included — the only way
		// to write "equals nothing": `== ` with nothing after it is refused
		// as a missing value, and an unquoted `==` against real text already
		// worked before this existed.
		{"quoted-empty-string-matches-empty", `{{inputs.empty}} == ""`, true},
		{"quoted-empty-string-does-not-match-non-empty", `{{inputs.status}} == ""`, false},
		{"quoted-literal-matches-its-text", `{{inputs.status}} == "overdue"`, true},
		{"quoted-literal-not-equal", `{{inputs.status}} != "paid"`, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := EvalCondition(c.expr, ctx)
			if err != nil {
				t.Fatalf("EvalCondition(%q) error: %v", c.expr, err)
			}
			if got != c.want {
				t.Errorf("EvalCondition(%q) = %v, want %v", c.expr, got, c.want)
			}
		})
	}
}

// Ordering two strings would silently answer a question nobody asked —
// stop.if already draws this same line at equality only.
func TestEvalConditionRefusesToOrderStrings(t *testing.T) {
	ctx := map[string]any{"inputs": map[string]any{"status": "overdue"}}
	if _, err := EvalCondition("{{inputs.status}} > paid", ctx); err == nil {
		t.Fatal("ordered two strings without complaint")
	}
}

func TestParseConditionPicksTheLongestOperator(t *testing.T) {
	cases := []struct {
		expr   string
		wantOp string
	}{
		{"{{inputs.a}} <= 5", "<="},
		{"{{inputs.a}} >= 5", ">="},
		{"{{inputs.a}} == 5", "=="},
		{"{{inputs.a}} != 5", "!="},
		{"{{inputs.a}} < 5", "<"},
		{"{{inputs.a}} > 5", ">"},
		{"{{inputs.a}}", ""},
	}
	for _, c := range cases {
		p, err := ParseCondition(c.expr)
		if err != nil {
			t.Fatalf("ParseCondition(%q) error: %v", c.expr, err)
		}
		if p.Op != c.wantOp {
			t.Errorf("ParseCondition(%q).Op = %q, want %q", c.expr, p.Op, c.wantOp)
		}
	}
}

func TestTemplatePathsStripsTheDefaultFilter(t *testing.T) {
	got := TemplatePaths("{{inputs.amount}} > {{inputs.threshold | default: 100}}")
	want := []string{"inputs.amount", "inputs.threshold"}
	if len(got) != len(want) {
		t.Fatalf("TemplatePaths = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("TemplatePaths[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
