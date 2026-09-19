package contract

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/redbotster/nanobots/internal/schema"
)

// The prose pages are one corpus: README.md plus every page in docs/.
//
// The README used to hold every claim these tests check. It reached 567
// lines that way, so the reference sections moved into docs/ — the test
// count to docs/testing.md, the catalog tables' prose to docs/status.md, the
// quoted YAML to docs/anatomy.md. The claims did not stop being claims by
// moving, so these tests follow them instead of pinning them to one file.
// Where a claim lives is a writing decision; whether it is true is not.
func docPages(t *testing.T, root string) []string {
	t.Helper()
	pages := []string{filepath.Join(root, "README.md")}
	docs, err := filepath.Glob(filepath.Join(root, "docs", "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	return append(pages, docs...)
}

// docsCorpus is every prose page concatenated, for the checks that only ask
// "is this stated anywhere a reader will find it".
func docsCorpus(t *testing.T, root string) string {
	t.Helper()
	var all strings.Builder
	for _, p := range docPages(t, root) {
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		all.Write(raw)
		all.WriteString("\n")
	}
	return all.String()
}

// The docs state how many tests this repo has, and print the command that
// produces the number so a reader can check it. That number drifted twice in
// one day of work — a claim nobody can be expected to hand-maintain is a
// claim that will be wrong.
func TestTheClaimedTestCountIsAccurate(t *testing.T) {
	root := repoRoot(t)
	corpus := docsCorpus(t, root)
	m := regexp.MustCompile(`(?m)^(\d+) table-driven Go tests`).FindStringSubmatch(corpus)
	if m == nil {
		t.Fatal("no page states a test count in the expected form " +
			"(`<n> table-driven Go tests` at the start of a line) — it lives in docs/testing.md")
	}
	claimed, err := strconv.Atoi(m[1])
	if err != nil {
		t.Fatal(err)
	}

	// Exactly the command the README prints, so the two can't disagree.
	cmd := exec.Command("bash", "-c",
		`grep -rho '^func Test[A-Za-z0-9_]*' --include='*_test.go' . | sort -u | wc -l`)
	cmd.Dir = root // the README's command is written to be run from the repo root
	out, err := cmd.Output()
	if err != nil {
		t.Skipf("could not count tests here: %v", err)
	}
	actual, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		t.Skipf("unexpected count output %q", out)
	}

	if claimed != actual {
		t.Errorf("the docs claim %d tests; there are %d. Update the number in docs/testing.md.", claimed, actual)
	}
}

// The docs also count bots and swarms. Same reasoning as the test count, and
// the same outcome without a check: both had drifted — 30 bots where there
// were 33, 14 swarms where there were 15 — because three review bots and a
// supervisor swarm were added without anyone re-counting. A number in prose
// is a claim, and an unchecked claim rots.
func TestTheHeadlineCatalogCountsAreAccurate(t *testing.T) {
	root := repoRoot(t)
	corpus := docsCorpus(t, root)

	for _, tc := range []struct {
		what    string
		pattern string
		count   func() int
	}{
		{
			"bots", `(?m)^\*\*(\d+) bots\*\*`,
			func() int { return len(allBotIDs(t, root)) },
		},
		{
			"swarms", `(?m)^\*\*(\d+) swarms\*\*`,
			func() int {
				files, _ := filepath.Glob(filepath.Join(root, "examples", "swarms", "*.yaml"))
				return len(files)
			},
		},
	} {
		m := regexp.MustCompile(tc.pattern).FindStringSubmatch(corpus)
		if m == nil {
			t.Errorf("no page states a %s count in the expected form (**<n> %s** at the start of a line)",
				tc.what, tc.what)
			continue
		}
		claimed, err := strconv.Atoi(m[1])
		if err != nil {
			t.Fatal(err)
		}
		if actual := tc.count(); claimed != actual {
			t.Errorf("the docs claim %d %s; there are %d.", claimed, tc.what, actual)
		}
	}
}

// The swarm table is the catalog's front door, and a swarm nobody added a row
// for is a swarm nobody finds. supervisor-review shipped without one.
func TestTheDocsNameEverySwarm(t *testing.T) {
	root := repoRoot(t)
	corpus := docsCorpus(t, root)
	files, err := filepath.Glob(filepath.Join(root, "examples", "swarms", "*.yaml"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no swarms found: %v", err)
	}
	for _, f := range files {
		name := strings.TrimSuffix(filepath.Base(f), ".yaml")
		if !strings.Contains(corpus, "`"+name+"`") {
			t.Errorf("swarm %q is named in no prose page — add a row to the README's table", name)
		}
	}
}

// Same for bots: the catalog list is how someone discovers what can snap
// into what, and the AI composer's prompt is built from the same directory.
func TestTheDocsNameEveryBot(t *testing.T) {
	root := repoRoot(t)
	corpus := docsCorpus(t, root)
	var missing []string
	for _, id := range allBotIDs(t, root) {
		if !strings.Contains(corpus, "`"+id+"`") {
			missing = append(missing, id)
		}
	}
	if len(missing) > 0 {
		t.Errorf("these bots are named in no prose page: %s", strings.Join(missing, ", "))
	}
}

// Every page in docs/ has to be reachable from the README by following
// links. docs/memory.md was the one that wasn't — written, linked from other
// docs, and invisible to anyone starting at the front page.
//
// This used to demand a direct `docs/<name>.md` mention in the README, which
// is what a 567-line README with a link to all of them looks like. The front
// page now links the ones most people want first and hands the rest to
// docs/README.md, so the check is reachability rather than adjacency: a page
// may be one hop away, it may not be zero hops from anywhere.
func TestEveryDocIsReachableFromTheReadme(t *testing.T) {
	root := repoRoot(t)

	// A page "links" another if it names the file at all — a markdown link,
	// a bare docs/<name>.md, or a backticked reference. All three are how
	// this repo's pages actually cross-reference each other, and all three
	// leave a reader somewhere they can follow.
	ref := regexp.MustCompile(`([A-Za-z0-9_-]+\.md)`)
	linksFrom := func(page string) []string {
		raw, err := os.ReadFile(page)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, m := range ref.FindAllStringSubmatch(string(raw), -1) {
			target := filepath.Join(root, "docs", m[1])
			if _, err := os.Stat(target); err == nil {
				out = append(out, target)
			}
		}
		return out
	}

	seen := map[string]bool{}
	queue := linksFrom(filepath.Join(root, "README.md"))
	for len(queue) > 0 {
		page := queue[0]
		queue = queue[1:]
		if seen[page] {
			continue
		}
		seen[page] = true
		queue = append(queue, linksFrom(page)...)
	}

	var unreachable []string
	docs, err := filepath.Glob(filepath.Join(root, "docs", "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range docs {
		if !seen[d] {
			unreachable = append(unreachable, filepath.Base(d))
		}
	}
	if len(unreachable) > 0 {
		t.Errorf("these docs cannot be reached by following links from the README, so nobody"+
			" starting at the front page will find them: %s\nAdd each to docs/README.md.",
			strings.Join(unreachable, ", "))
	}
}

// "Eleven of the fifteen catalog swarms carry a cron trigger" is repeated
// in five files — a CLI help note, the service installer's doc comment, the
// builder, and twice in docs/scheduler.md. It was "fourteen" in all of them
// for a long time, and wrong: the real split is 11 cron, 2 event, 1 webhook,
// 1 manual. A number nobody can check is a number that drifts.
func TestTheClaimedCronSwarmCountIsAccurate(t *testing.T) {
	root := repoRoot(t)
	entries, err := os.ReadDir(filepath.Join(root, "examples", "swarms"))
	if err != nil {
		t.Fatal(err)
	}
	cron, total := 0, 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		total++
		raw, err := os.ReadFile(filepath.Join(root, "examples", "swarms", e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "type: cron") {
			cron++
		}
	}

	// Sentence case: the count opens a sentence, the total does not.
	claim := fmt.Sprintf("%s of the %s catalog swarms carry a cron trigger",
		strings.ToUpper(numberWord(cron)[:1])+numberWord(cron)[1:], numberWord(total))
	// Where a claim lives is a writing decision; whether it is true is not.
	// This one moved from cmd/nanobots/main.go to service.go when that
	// 1203-line file was split one command per file, and the test followed
	// it rather than the file being kept fat to satisfy the test.
	for _, f := range []string{
		"cmd/nanobots/service.go",
		"internal/service/service.go",
		"internal/api/builder.go",
		"docs/scheduler.md",
	} {
		raw, err := os.ReadFile(filepath.Join(root, f))
		if err != nil {
			t.Fatal(err)
		}
		// The comment wraps, so compare on whitespace-collapsed text.
		flat := strings.Join(strings.Fields(strings.ReplaceAll(string(raw), "//", " ")), " ")
		if !strings.Contains(flat, claim) {
			t.Errorf("%s does not say %q — there are %d cron swarms of %d", f, claim, cron, total)
		}
	}
}

// CLAUDE.md's own repo-shape section spells out the full trigger split
// ("15 cron, 1 webhook, 2 manual"), which TestTheClaimedCronSwarmCountIsAccurate
// doesn't check — that test only follows the cron half, and only into the
// files where it found "cron" already; the sentence containing all three
// numbers lives nowhere that test reads. v3's Phase 0 asked for every stated
// count anywhere in README.md, CLAUDE.md and docs/ to be checked, and this
// specific breakdown was the one still unchecked at the time — verified
// accurate by hand against real trigger types before adding the test that
// keeps it that way.
func TestClaudeMdsTriggerBreakdownIsAccurate(t *testing.T) {
	root := repoRoot(t)
	files, err := filepath.Glob(filepath.Join(root, "examples", "swarms", "*.yaml"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no swarms found: %v", err)
	}
	counts := map[string]int{}
	for _, f := range files {
		sw, err := schema.LoadNanoswarm(f)
		if err != nil {
			t.Fatalf("load %s: %v", f, err)
		}
		counts[sw.Spec.Trigger.Type]++
	}

	claim := fmt.Sprintf("`examples/swarms/` — %d swarms: %d cron, %d webhook, %d manual.",
		len(files), counts["cron"], counts["webhook"], counts["manual"])
	raw, err := os.ReadFile(filepath.Join(root, "CLAUDE.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), claim) {
		t.Errorf("CLAUDE.md's repo-shape section does not say %q — real trigger counts: %v", claim, counts)
	}
}

// numberWord spells the small numbers these comments use.
func numberWord(n int) string {
	words := []string{"zero", "one", "two", "three", "four", "five", "six", "seven",
		"eight", "nine", "ten", "eleven", "twelve", "thirteen", "fourteen", "fifteen",
		"sixteen", "seventeen", "eighteen", "nineteen", "twenty"}
	if n < len(words) {
		return words[n]
	}
	return fmt.Sprint(n)
}

// Every catalog count in the README, not just the two headline ones.
//
// TestTheHeadlineCatalogCountsAreAccurate only matches a bolded count at the
// start of a line, so four prose claims sat at "30 nanobots, 14 nanoswarms"
// through nine bots and two swarms being added — including the sentence
// that introduces the whole catalog. A number the tests do not read is a
// number that drifts.
//
// Deliberately absolute: no allowlist, no historical exceptions. Prose that
// needs to say a different number spells it out in words ("the two bricks
// that predate it"), which is also how it reads better.
func TestEveryDocCountIsCurrent(t *testing.T) {
	root := repoRoot(t)
	readme, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	files, _ := filepath.Glob(filepath.Join(root, "examples", "swarms", "*.yaml"))
	want := map[string]int{
		"bots":       len(allBotIDs(t, root)),
		"nanobots":   len(allBotIDs(t, root)),
		"swarms":     len(files),
		"nanoswarms": len(files),
	}

	// Every prose page, not just the README. The same drift was in
	// docs/harnesses.md ("25 of 30 bots"), docs/connections.md ("all 14
	// swarms") and CLAUDE.md, which had gone stale within a day of being
	// written.
	pages := []string{"README.md", "CLAUDE.md"}
	docs, _ := filepath.Glob(filepath.Join(root, "docs", "*.md"))
	for _, d := range docs {
		rel, err := filepath.Rel(root, d)
		if err == nil {
			pages = append(pages, rel)
		}
	}
	_ = readme

	re := regexp.MustCompile(`(\d+) (nanobots|nanoswarms|bots|swarms)\b`)
	for _, page := range pages {
		raw, err := os.ReadFile(filepath.Join(root, page))
		if err != nil {
			continue
		}
		// Whitespace collapsed first, because prose wraps. "**39 nanobots,
		// 16\nnanoswarms**" hid a stale count from this test through two
		// swarms being added — the number and its noun sat either side of a
		// line break, so the pattern never saw them together.
		flat := strings.Join(strings.Fields(stripFences(string(raw))), " ")
		for _, m := range re.FindAllStringSubmatch(flat, -1) {
			got, err := strconv.Atoi(m[1])
			if err != nil {
				continue
			}
			if got != want[m[2]] {
				t.Errorf("%s says %q; there are %d %s. Spell a historical number "+
					"as a word if it is deliberately not the current count.",
					page, m[0], want[m[2]], m[2])
			}
		}
	}
}

// stripFences blanks out fenced code blocks. What is inside one is a
// transcript or a mock of rendered output — docs/connections.md shows a
// Settings row reading "18 bots would use this once connected" — and an
// illustration is allowed to be an illustration. Prose making a claim about
// the catalog is not.
func stripFences(s string) string {
	var out strings.Builder
	inFence := false
	for _, line := range strings.Split(s, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inFence = !inFence
			continue
		}
		if !inFence {
			out.WriteString(line)
		}
		out.WriteString("\n")
	}
	return out.String()
}

// The README grew reference material once already — it reached 567 lines
// before the test count, the catalog tables' prose and the real/simulated
// breakdown moved into docs/ pages that already existed. Nothing stopped it
// creeping back except someone noticing, so v3's Phase 0 asked for a test
// that notices instead. 250 is not a hard architectural limit; it is a
// trip-wire a bit above the trimmed size (249 lines when this was added) —
// low enough to fire before the next long section accretes, high enough
// that "what it is, why bricks, install, the catalog table, links" fits
// without a fight.
func TestTheReadmeStaysUnderTwoHundredFiftyLines(t *testing.T) {
	root := repoRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Count(string(raw), "\n")
	const max = 250
	if lines > max {
		t.Errorf("README.md is %d lines, over the %d-line cap. Move the new "+
			"reference material into a docs/ page (existing pages: docs/README.md's "+
			"index) rather than raising this number.", lines, max)
	}
}

// No bot's demo output may contain an em dash.
//
// Those fixtures are what a bot is shown to produce, and on a fresh install
// they are the only output anyone sees. Twenty-two of them had one, across
// twelve bots, while the `tone` bot existed specifically to remove them —
// the catalog was demonstrating the habit it ships a tool to fix.
//
// inputs.json and service fixtures are deliberately not checked: those
// stand in for text a person or a third-party API wrote, and people use em
// dashes.
func TestNoBotDemoOutputUsesAnEmDash(t *testing.T) {
	root := repoRoot(t)
	entries, err := os.ReadDir(filepath.Join(root, "bots"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		path := filepath.Join(root, "bots", e.Name(), "fixtures", "ai.generate.json")
		raw, err := os.ReadFile(path)
		if err != nil {
			continue // not every bot generates
		}
		if n := strings.Count(string(raw), "—"); n > 0 {
			t.Errorf("%s/fixtures/ai.generate.json has %d em dash(es) — this is demo output, "+
				"and the catalog should not model the habit `tone` exists to remove", e.Name(), n)
		}
	}
}

// Every prose bot must tell the model the house voice, or it learns the
// habit from the prompt's own style. Only the four bots written alongside
// `tone` said anything; the other twenty-three said nothing.
func TestEveryProsePromptStatesTheHouseVoice(t *testing.T) {
	root := repoRoot(t)
	entries, err := os.ReadDir(filepath.Join(root, "bots"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		spec, err := os.ReadFile(filepath.Join(root, "bots", e.Name(), "nanobot.yaml"))
		if err != nil || !strings.Contains(string(spec), "type: ai.generate") {
			continue
		}
		prompts, _ := filepath.Glob(filepath.Join(root, "bots", e.Name(), "prompts", "*.md"))
		var all strings.Builder
		for _, p := range prompts {
			raw, _ := os.ReadFile(p)
			all.Write(raw)
		}
		if !strings.Contains(strings.ToLower(all.String()), "no em dashes") {
			t.Errorf("%s generates prose but no prompt tells it the house voice", e.Name())
		}
	}
}
