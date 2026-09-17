package contract

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// Two bots' fixtures for the same service op must agree about its shape.
//
// A fixture stands in for a real API response, and `nanobots conform`
// replays it offline. When one bot's copy has a field another's does not,
// at least one of them is describing an API that does not exist — and the
// failure that produces is the worst kind, because demo mode stays green
// and the truth only arrives when someone connects a real account.
//
// The real case: `gmail.drafts.create` returns `{draft_ids, drafts}` from
// internal/step's dispatcher. `invoice-chaser` and `draft-replies` had
// both; `calendar-scheduler`, `follow-up-chaser` and `newsletter-drafter`
// had only `draft_ids`. Anyone writing a bot against one of those three
// would have concluded that `drafts` does not exist.
//
// Deliberately a consistency check rather than a comparison against the Go
// source. Parsing the dispatchers to learn their return shape couples this
// to how they are written; "the catalog's own copies disagree" needs no
// coupling at all and is the same evidence a reader would use.
func TestFixturesForOneOpAgreeOnItsShape(t *testing.T) {
	root := repoRoot(t)
	// op -> bot -> top-level keys
	shapes := map[string]map[string][]string{}

	bots, err := os.ReadDir(filepath.Join(root, "bots"))
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range bots {
		if !b.IsDir() {
			continue
		}
		dir := filepath.Join(root, "bots", b.Name(), "fixtures")
		files, _ := os.ReadDir(dir)
		for _, f := range files {
			name := f.Name()
			// <service>.<op>.json. inputs.json and ai.generate.json are a
			// bot's own data, not an API's answer.
			if !strings.HasSuffix(name, ".json") || name == "inputs.json" || name == "ai.generate.json" {
				continue
			}
			op := strings.TrimSuffix(name, ".json")
			if strings.Count(op, ".") < 2 {
				continue
			}
			raw, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				t.Fatal(err)
			}
			var doc map[string]any
			if err := json.Unmarshal(raw, &doc); err != nil {
				continue // a list-shaped fixture has no top-level keys to compare
			}
			keys := make([]string, 0, len(doc))
			for k := range doc {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			if shapes[op] == nil {
				shapes[op] = map[string][]string{}
			}
			shapes[op][b.Name()] = keys
		}
	}

	checked := 0
	for op, byBot := range shapes {
		if len(byBot) < 2 {
			continue // nothing to disagree with
		}
		checked++
		// The union is the shape the op really has: a key one bot's fixture
		// carries is a key the API returns.
		union := map[string]bool{}
		for _, keys := range byBot {
			for _, k := range keys {
				union[k] = true
			}
		}
		for bot, keys := range byBot {
			have := map[string]bool{}
			for _, k := range keys {
				have[k] = true
			}
			var missing []string
			for k := range union {
				if !have[k] {
					missing = append(missing, k)
				}
			}
			sort.Strings(missing)
			if len(missing) > 0 {
				t.Errorf("bots/%s/fixtures/%s.json is missing %v, which another bot's fixture for "+
					"the same op has — one of them describes an API that does not exist",
					bot, op, missing)
			}
		}
	}
	if checked == 0 {
		t.Error("no op is used by two bots, so this test compared nothing")
	}
}
