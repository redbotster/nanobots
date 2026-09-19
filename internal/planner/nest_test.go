package planner

import (
	"os"
	"path/filepath"
	"testing"
)

// A minimal nanobot.yaml with the given ports and one no-op step — same
// shape writeFallbackBot uses, kept local to this file so nesting tests
// don't depend on the fallback test's naming.
func writeNestBot(t *testing.T, botsDir, name, ports string) {
	t.Helper()
	writeFallbackBot(t, botsDir, name, ports)
}

func writeNestedSwarm(t *testing.T, dir, filename, body string) string {
	t.Helper()
	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// The smallest real case: one swarm nests another one-bot swarm, wired on
// both sides. Downstream and upstream bots never know they're snapped to
// something that used to be a whole other file.
func TestANestedSwarmInlinesCleanAndPlans(t *testing.T) {
	botsDir := t.TempDir()
	writeNestBot(t, botsDir, "source", `    outputs:
      - name: ticket_id
        type: string`)
	writeNestBot(t, botsDir, "lookup", `    inputs:
      - name: email
        type: string
        required: true
    outputs:
      - name: summary
        type: string`)
	writeNestBot(t, botsDir, "sink", `    inputs:
      - name: message
        type: string
        required: true`)

	dir := t.TempDir()
	writeNestedSwarm(t, dir, "inner.yaml", `apiVersion: nanobots.dev/v1alpha1
kind: Nanoswarm
metadata:
  name: inner
  description: inner
spec:
  ports:
    inputs:
      - name: customer_email
        type: string
        maps_to: lookup.email
    outputs:
      - name: result
        type: string
        maps_to: lookup.summary
  bots:
    - id: lookup
      use: lookup@0.1.0
  snaps: []
`)
	outerPath := writeNestedSwarm(t, dir, "outer.yaml", `apiVersion: nanobots.dev/v1alpha1
kind: Nanoswarm
metadata:
  name: outer
  description: outer
spec:
  bots:
    - id: intake
      use: source@0.1.0
    - id: refund
      swarm: ./inner.yaml
    - id: notifier
      use: sink@0.1.0
  snaps:
    - from: intake.ticket_id
      to: refund.customer_email
    - from: refund.result
      to: notifier.message
`)

	res, err := Plan(outerPath, botsDir)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if !res.OK() {
		t.Fatalf("nested swarm did not plan clean:\n%s", res.Report())
	}
	if _, ok := res.Resolved.Bots["refund/lookup"]; !ok {
		var ids []string
		for id := range res.Resolved.Bots {
			ids = append(ids, id)
		}
		t.Fatalf("inlined bot missing; have %v", ids)
	}
	if _, ok := res.Resolved.Bots["refund"]; ok {
		t.Fatal("the swarm: node itself should not survive inlining as a bot")
	}
}

// A swarm with no declared ports has no typed boundary to snap into and
// can't be nested — the same reason an undeclared bot port can't be
// snapped either.
func TestNestingASwarmWithNoPortsIsRejected(t *testing.T) {
	botsDir := t.TempDir()
	writeNestBot(t, botsDir, "lookup", `    outputs:
      - name: summary
        type: string`)

	dir := t.TempDir()
	writeNestedSwarm(t, dir, "inner.yaml", `apiVersion: nanobots.dev/v1alpha1
kind: Nanoswarm
metadata:
  name: inner
  description: inner
spec:
  bots:
    - id: lookup
      use: lookup@0.1.0
  snaps: []
`)
	outerPath := writeNestedSwarm(t, dir, "outer.yaml", `apiVersion: nanobots.dev/v1alpha1
kind: Nanoswarm
metadata:
  name: outer
  description: outer
spec:
  bots:
    - id: refund
      swarm: ./inner.yaml
  snaps: []
`)
	if _, err := Plan(outerPath, botsDir); err == nil {
		t.Fatal("a swarm with no declared ports nested without complaint")
	}
}

// A nests B nests A: the chain is caught rather than looping forever.
func TestASwarmNestingItselfIsRejected(t *testing.T) {
	botsDir := t.TempDir()
	dir := t.TempDir()
	writeNestedSwarm(t, dir, "a.yaml", `apiVersion: nanobots.dev/v1alpha1
kind: Nanoswarm
metadata:
  name: a
  description: a
spec:
  ports:
    inputs:
      - name: x
        type: string
        maps_to: b.x
  bots:
    - id: b
      swarm: ./b.yaml
  snaps: []
`)
	aPath := writeNestedSwarm(t, dir, "b.yaml", `apiVersion: nanobots.dev/v1alpha1
kind: Nanoswarm
metadata:
  name: b
  description: b
spec:
  ports:
    inputs:
      - name: x
        type: string
        maps_to: c.x
  bots:
    - id: c
      swarm: ./a.yaml
  snaps: []
`)
	_ = aPath
	if _, err := Plan(filepath.Join(dir, "a.yaml"), botsDir); err == nil {
		t.Fatal("a swarm nesting itself through another one planned without complaint")
	}
}

// swarm: can't combine with the per-instance controls that assume there is
// one bot to apply them to — once inlined, a nested swarm is N bots.
func TestSwarmCannotCombineWithRetryOrWhenOrLoopOrFallback(t *testing.T) {
	botsDir := t.TempDir()
	writeNestBot(t, botsDir, "lookup", `    outputs:
      - name: summary
        type: string`)

	dir := t.TempDir()
	writeNestedSwarm(t, dir, "inner.yaml", `apiVersion: nanobots.dev/v1alpha1
kind: Nanoswarm
metadata:
  name: inner
  description: inner
spec:
  ports:
    outputs:
      - name: result
        type: string
        maps_to: lookup.summary
  bots:
    - id: lookup
      use: lookup@0.1.0
  snaps: []
`)

	for name, extra := range map[string]string{
		"retry":    "      retry: 2\n",
		"when":     "      when: \"{{inputs.x}} == 1\"\n",
		"loop":     "      loop:\n        max: 3\n",
		"fallback": "      fallback: lookup@0.1.0\n",
	} {
		t.Run(name, func(t *testing.T) {
			outerPath := writeNestedSwarm(t, dir, "outer-"+name+".yaml", `apiVersion: nanobots.dev/v1alpha1
kind: Nanoswarm
metadata:
  name: outer
  description: outer
spec:
  bots:
    - id: refund
      swarm: ./inner.yaml
`+extra+`  snaps: []
`)
			if _, err := Plan(outerPath, botsDir); err == nil {
				t.Fatalf("swarm: combined with %s planned without complaint", name)
			}
		})
	}
}

// A bot id can't carry "." or "/" — both are reserved, one by the snap
// syntax, one by nested-swarm inlining.
func TestABotIDCannotContainDotOrSlash(t *testing.T) {
	botsDir := t.TempDir()
	writeNestBot(t, botsDir, "lookup", `    outputs:
      - name: summary
        type: string`)

	for _, id := range []string{"a.b", "a/b"} {
		dir := t.TempDir()
		path := writeNestedSwarm(t, dir, "s.yaml", `apiVersion: nanobots.dev/v1alpha1
kind: Nanoswarm
metadata:
  name: s
  description: s
spec:
  bots:
    - id: `+id+`
      use: lookup@0.1.0
  snaps: []
`)
		if _, err := Plan(path, botsDir); err == nil {
			t.Errorf("bot id %q accepted without complaint", id)
		}
	}
}
