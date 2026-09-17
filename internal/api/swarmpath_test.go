package api

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Every handler that acts on a named swarm goes through this one function —
// POST /api/runs, the plan endpoints, the builder's load and save — so the
// awkward inputs are worth enumerating in one place.
//
// The empty string is the one that shipped broken. `POST /api/runs` with no
// swarm_path answered `read /app/examples/swarms: is a directory`, because
// filepath.Base("") is "." and the join therefore resolved to the swarms
// directory itself, which exists. Found by curling the container with the
// field name spelled wrong.
func TestSwarmPathFromRequest(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "morning-brief.yaml")
	if err := os.WriteFile(real, []byte("kind: Nanoswarm\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "a-directory.yaml"), 0o755); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name    string
		in      string
		want    string
		wantErr string
	}{
		{name: "a plain file name", in: "morning-brief.yaml", want: real},
		{
			name: "a directory component is stripped, not honoured",
			in:   "../../etc/morning-brief.yaml", want: real,
		},
		{name: "nothing at all", in: "", wantErr: "no swarm named"},
		{name: "a bare dot", in: ".", wantErr: "no swarm named"},
		{name: "a bare dotdot", in: "..", wantErr: "no swarm named"},
		{name: "the root", in: "/", wantErr: "no swarm named"},
		{name: "a name that is a directory", in: "a-directory.yaml", wantErr: "is not a file"},
		{name: "a name that is not there", in: "nope.yaml", wantErr: "not found"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := swarmPathFromRequest(dir, tc.in)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("got %q, want an error mentioning %q", got, tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Errorf("error = %v, want it to mention %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}
