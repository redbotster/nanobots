package contract

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// docs/anatomy.md quotes a real bot and a real swarm, so a reader can see
// the two artifacts the whole system is made of without opening the repo.
//
// Quoted YAML is the kind of thing that rots quietly: someone edits the bot,
// the page keeps showing last month's ports, and the first thing a new
// reader learns is that the docs lie. The same argument as the test-count
// and catalog-count checks in this package — if it is a claim, it checks
// itself.
//
// Checked against every prose page rather than one file: these two blocks
// lived in the README until it was split into docs/, and pinning the check
// to a filename would have made moving them look like deleting them.
func TestTheDocsQuoteTheRealFiles(t *testing.T) {
	root := repoRoot(t)
	corpus := docsCorpus(t, root)

	for _, tc := range []struct {
		file  string
		from  string // "" means the whole file
		until string
		what  string
	}{
		{
			file: filepath.Join("examples", "swarms", "github-digest-to-slack.yaml"),
			what: "the example swarm",
		},
		{
			file:  filepath.Join("bots", "github-issues-digest", "nanobot.yaml"),
			from:  "  ports:",
			until: "  guardrails:",
			what:  "the example bot's ports and steps",
		},
	} {
		body := readFile(t, filepath.Join(root, tc.file))
		want := body
		if tc.from != "" {
			i := strings.Index(body, tc.from)
			j := strings.Index(body, tc.until)
			if i < 0 || j < 0 || j <= i {
				t.Fatalf("%s no longer has the %q..%q block this test slices", tc.file, tc.from, tc.until)
			}
			want = body[i:j]
		}
		want = strings.TrimRight(want, "\n")

		if !strings.Contains(corpus, want) {
			t.Errorf("the docs' copy of %s (%s) no longer matches the file.\n"+
				"Re-copy it into docs/anatomy.md, or quote a smaller slice.",
				tc.what, tc.file)
		}
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}
