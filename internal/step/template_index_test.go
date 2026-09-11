package step

import "testing"

func TestListIndex(t *testing.T) {
	cases := []struct {
		in      string
		wantIdx int
		wantOK  bool
	}{
		{"0", 0, true},
		{"12", 12, true},
		{"", 0, false},
		{"draft_ids", 0, false},
		{"-1", 0, false},
		{"01", 1, true}, // leading zero is still a valid index
	}
	for _, c := range cases {
		idx, ok := ListIndex(c.in)
		if ok != c.wantOK || (ok && idx != c.wantIdx) {
			t.Errorf("ListIndex(%q) = %d, %v, want %d, %v", c.in, idx, ok, c.wantIdx, c.wantOK)
		}
	}
}

func TestLookupPathIndexesIntoLists(t *testing.T) {
	ctx := map[string]any{
		"steps": map[string]any{
			"draft": map[string]any{
				"output": []any{"d-1", "d-2", "d-3"},
			},
		},
	}
	got, ok := lookupPath(ctx, "steps.draft.output.1")
	if !ok || got != "d-2" {
		t.Errorf("lookupPath(...output.1) = %v, %v, want d-2, true", got, ok)
	}
	if _, ok := lookupPath(ctx, "steps.draft.output.9"); ok {
		t.Error("expected an out-of-range index to report not found")
	}
}
