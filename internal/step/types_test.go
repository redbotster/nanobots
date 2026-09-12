package step

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// step.Types() is the list everything else is checked against — the bot
// contract doc, the foundry's brief, error messages offering alternatives.
// It is hand-maintained, so it has to be checked against the one thing that
// cannot lie: the interpreter's own switch.
func TestTypesMatchesTheInterpretersSwitch(t *testing.T) {
	raw, err := os.ReadFile("interpret.go")
	if err != nil {
		t.Fatal(err)
	}
	var inSwitch []string
	for _, m := range regexp.MustCompile(`(?m)^\s*case "([a-z._]+)":`).FindAllStringSubmatch(string(raw), -1) {
		inSwitch = append(inSwitch, m[1])
	}
	if len(inSwitch) == 0 {
		t.Fatal("found no step cases — did the interpreter's switch move?")
	}

	declared := Types()
	sort.Strings(declared)
	sort.Strings(inSwitch)

	if strings.Join(declared, ",") != strings.Join(inSwitch, ",") {
		t.Errorf("step.Types() and the interpreter disagree.\n  Types():     %v\n  interpreter: %v",
			declared, inSwitch)
	}
}
