package schema

import "testing"

func TestParsePortType(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"string", "string", false},
		{"datetime", "datetime", false},
		{"list<string>", "list<string>", false},
		{"list<json>", "list<json>", false},
		{"list<list<string>>", "list<list<string>>", false},
		{"file?", "file", false}, // trailing "?" marks an optional port inline
		{"bogus", "", true},
	}
	for _, c := range cases {
		got, err := ParsePortType(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("ParsePortType(%q): expected error, got %v", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Fatalf("ParsePortType(%q): unexpected error: %v", c.in, err)
		}
		if got.String() != c.want {
			t.Errorf("ParsePortType(%q) = %q, want %q", c.in, got.String(), c.want)
		}
	}
}

func TestAssignable(t *testing.T) {
	mustParse := func(s string) ParsedType {
		p, err := ParsePortType(s)
		if err != nil {
			t.Fatalf("ParsePortType(%q): %v", s, err)
		}
		return p
	}
	cases := []struct {
		from, to string
		want     bool
	}{
		{"string", "string", true},
		{"string", "datetime", false},
		{"json", "json", true},
		{"list<string>", "list<string>", true},
		{"list<string>", "list<json>", false},
		{"list<json>", "string", false},
		{"file", "file", true},
	}
	for _, c := range cases {
		got := Assignable(mustParse(c.from), mustParse(c.to))
		if got != c.want {
			t.Errorf("Assignable(%q, %q) = %v, want %v", c.from, c.to, got, c.want)
		}
	}
}
