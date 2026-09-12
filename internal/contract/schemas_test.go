package contract

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/redbotster/nanobots/internal/step"
)

// schemas/*.json is generated from the Go types by `nanobots schema`, and
// it is what a user's editor validates their YAML against.
//
// Both files had drifted: nanoswarm.schema.json knew nothing about
// `on_error` or `join`, and nanobot.schema.json nothing about `optional`,
// `query`, `data` or a step's `outputs` map. Every one of those is a real
// field the runner honours — so someone writing correct YAML got a red
// squiggle telling them it was invalid, which is a worse failure than no
// schema at all.
//
// A generated file nobody regenerates is a stale file. This makes the repo
// check, the same way it checks its own test count.
func TestGeneratedSchemasAreUpToDate(t *testing.T) {
	root := repoRoot(t)
	out := t.TempDir()

	cmd := exec.Command("go", "run", "./cmd/nanobots", "schema", "--out", out)
	cmd.Dir = root
	if combined, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("could not regenerate here: %v: %s", err, combined)
	}

	for _, name := range []string{"nanobot.schema.json", "nanoswarm.schema.json"} {
		committed, err := os.ReadFile(filepath.Join(root, "schemas", name))
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		fresh, err := os.ReadFile(filepath.Join(out, name))
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		// Compare parsed, not byte-for-byte: key order and trailing
		// whitespace are not what anyone means by "out of date".
		if !sameJSON(t, committed, fresh) {
			t.Errorf("schemas/%s is out of date with the Go types.\n"+
				"Run: go run ./cmd/nanobots schema --out schemas", name)
		}
	}
}

func sameJSON(t *testing.T, a, b []byte) bool {
	t.Helper()
	var av, bv any
	if err := json.Unmarshal(a, &av); err != nil {
		return false
	}
	if err := json.Unmarshal(b, &bv); err != nil {
		return false
	}
	an, _ := json.Marshal(av)
	bn, _ := json.Marshal(bv)
	return bytes.Equal(an, bn)
}

// docs/bot-contract.md is not just documentation: internal/foundry feeds it
// verbatim to a coding agent authoring a brand-new bot. A step type missing
// from it is a step type that agent has never heard of.
//
// It listed six for a long time, so a foundry-authored bot could not have
// used memory.recall, memory.remember, or any of the three transforms.
func TestTheBotContractDocNamesEveryStepType(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), "docs", "bot-contract.md"))
	if err != nil {
		t.Fatal(err)
	}
	doc := string(raw)
	for _, typ := range step.Types() {
		if !strings.Contains(doc, "`"+typ+"`") {
			t.Errorf("docs/bot-contract.md never mentions the %q step, and the foundry hands this"+
				" file to an agent writing new bots", typ)
		}
	}
}
