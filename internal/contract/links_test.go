package contract

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Every relative markdown link in a prose page has to point at a file that
// exists.
//
// Splitting a 567-line README into docs/ moved twenty-odd sections behind
// links, and a link is a claim like any other: `[error-policy.md](error-policy.md)`
// from inside docs/ resolves differently to `docs/error-policy.md` from the
// root, and getting that wrong produces a 404 that no build, vet or test
// would otherwise notice. Renaming a page later has the same failure mode,
// which is the one worth guarding — this repo renames pages.
//
// External links are not checked: a test that fails when someone else's
// site is down is a test people learn to ignore.
func TestEveryRelativeDocLinkResolves(t *testing.T) {
	root := repoRoot(t)
	link := regexp.MustCompile(`\]\(([^)]+)\)`)

	for _, page := range docPages(t, root) {
		raw, err := os.ReadFile(page)
		if err != nil {
			t.Fatal(err)
		}
		dir := filepath.Dir(page)
		for _, m := range link.FindAllStringSubmatch(string(raw), -1) {
			target := m[1]
			if strings.HasPrefix(target, "http") || strings.HasPrefix(target, "#") ||
				strings.HasPrefix(target, "mailto:") {
				continue
			}
			// A trailing #anchor addresses a heading in the same file.
			if i := strings.Index(target, "#"); i >= 0 {
				target = target[:i]
			}
			if target == "" {
				continue
			}
			if _, err := os.Stat(filepath.Join(dir, target)); err != nil {
				rel, _ := filepath.Rel(root, page)
				t.Errorf("%s links to %q, which does not exist from there", rel, m[1])
			}
		}
	}
}
