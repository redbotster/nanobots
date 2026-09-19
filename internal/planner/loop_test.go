package planner

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func writeLoopBot(t *testing.T, botsDir, name, ports string) {
	t.Helper()
	writeFallbackBot(t, botsDir, name, ports) // same minimal shape, different name
}

// A loop with a sane max, a feed: mapping between real ports, and an
// until: that only tests this bot's own outputs plans clean.
func TestALoopWithGoodShapePlansClean(t *testing.T) {
	botsDir := t.TempDir()
	writeLoopBot(t, botsDir, "paginate", `    inputs:
      - name: page_token
        type: string
    outputs:
      - name: next_page_token
        type: string
      - name: items
        type: list<json>`)

	res := planFallback(t, botsDir, `  bots:
    - id: fetch
      use: paginate@0.1.0
      loop:
        max: 20
        feed: { page_token: next_page_token }
        until: "{{outputs.next_page_token}} == \"\""
`)
	if errs := fallbackErrs(res); len(errs) > 0 {
		t.Fatalf("a well-shaped loop was rejected: %v", errs)
	}
}

func TestALoopMaxOutsideOneToTwentyIsRejected(t *testing.T) {
	botsDir := t.TempDir()
	writeLoopBot(t, botsDir, "paginate", `    outputs:
      - name: next_page_token
        type: string`)

	for _, max := range []int{0, -1, 21, 1000} {
		res := planFallback(t, botsDir, fmtLoopSwarm("fetch", "paginate@0.1.0", max))
		if !containsSubstr(fallbackErrs(res), "must be between 1 and 20") {
			t.Errorf("max=%d: expected a range error, got %v", max, fallbackErrs(res))
		}
	}
}

func fmtLoopSwarm(id, use string, max int) string {
	return `  bots:
    - id: ` + id + `
      use: ` + use + `
      loop:
        max: ` + strconv.Itoa(max) + `
`
}

func TestALoopFeedNamingAPortThatDoesNotExistIsRejected(t *testing.T) {
	botsDir := t.TempDir()
	writeLoopBot(t, botsDir, "paginate", `    inputs:
      - name: page_token
        type: string
    outputs:
      - name: next_page_token
        type: string`)

	res := planFallback(t, botsDir, `  bots:
    - id: fetch
      use: paginate@0.1.0
      loop:
        max: 5
        feed: { no_such_input: next_page_token }
`)
	if !containsSubstr(fallbackErrs(res), `"no_such_input" is not one of its own input ports`) {
		t.Fatalf("expected a bad-feed-input error, got %v", fallbackErrs(res))
	}

	res2 := planFallback(t, botsDir, `  bots:
    - id: fetch
      use: paginate@0.1.0
      loop:
        max: 5
        feed: { page_token: no_such_output }
`)
	if !containsSubstr(fallbackErrs(res2), `"no_such_output" is not one of its own output ports`) {
		t.Fatalf("expected a bad-feed-output error, got %v", fallbackErrs(res2))
	}
}

func TestALoopUntilReachingPastThisBotsOutputsIsRejected(t *testing.T) {
	botsDir := t.TempDir()
	writeLoopBot(t, botsDir, "paginate", `    outputs:
      - name: next_page_token
        type: string`)

	res := planFallback(t, botsDir, `  bots:
    - id: fetch
      use: paginate@0.1.0
      loop:
        max: 5
        until: "{{inputs.page_token}} == \"\""
`)
	if !containsSubstr(fallbackErrs(res), "not one of this bot's own outputs") {
		t.Fatalf("expected an until-scope error, got %v", fallbackErrs(res))
	}
}

// loop: and a fanned-out snap can't both apply to the same bot instance —
// they disagree about how many of its outputs downstream sees.
func TestALoopAndFanOutOnTheSameBotIsRejected(t *testing.T) {
	botsDir := t.TempDir()
	writeLoopBot(t, botsDir, "source", `    outputs:
      - name: items
        type: list<string>`)
	writeLoopBot(t, botsDir, "paginate", `    inputs:
      - name: item
        type: string
    outputs:
      - name: next_page_token
        type: string`)

	path := filepath.Join(t.TempDir(), "probe.yaml")
	body := `apiVersion: nanobots.dev/v1alpha1
kind: Nanoswarm
metadata:
  name: probe
  description: probe
spec:
  bots:
    - id: source
      use: source@0.1.0
    - id: fetch
      use: paginate@0.1.0
      loop:
        max: 5
  snaps:
    - from: source.items.*
      to: fetch.item
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Plan(path, botsDir)
	if err != nil {
		t.Fatal(err)
	}
	if !containsSubstr(fallbackErrs(got), "both loop: and a fanned-out snap") {
		t.Fatalf("expected a loop+fanout conflict error, got %v", fallbackErrs(got))
	}
}
