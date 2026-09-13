package planner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func planYAML(t *testing.T, body string) *PlanResult {
	t.Helper()
	root := repoRootForLevels(t)
	path := filepath.Join(t.TempDir(), "probe.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	res, err := Plan(path, filepath.Join(root, "bots"))
	if err != nil {
		t.Fatal(err)
	}
	return res
}

const unfedHeader = `apiVersion: nanobots.dev/v1alpha1
kind: Nanoswarm
metadata:
  name: probe
  description: probe
spec:
`

// A required input with no snap, no explicit value and no default is a
// plan-time fact — nothing about it depends on data — and it used to
// surface only at run time, several containers in. lead-to-meeting shipped
// that way and `nanobots plan` said OK, because the snaps type-checked and
// the DAG had no cycles, and neither of those questions is "does every bot
// have what it needs".
func TestARequiredInputWithNoSourceFailsThePlan(t *testing.T) {
	res := planYAML(t, unfedHeader+`  bots:
    - id: mailer
      use: email-drive-file@1.1.0
  snaps: []
`)
	if res.OK() {
		t.Fatal("a swarm with an unfillable required input planned OK")
	}
	var names []string
	for _, u := range res.Unfed {
		names = append(names, u.Port)
	}
	// email-drive-file requires both.
	for _, want := range []string{"file_id", "to"} {
		if !contains(names, want) {
			t.Errorf("%q not reported as unfed; got %v", want, names)
		}
	}
}

// All three ways of satisfying one must count.
func TestEveryWayOfFillingARequiredInputCounts(t *testing.T) {
	// An explicit value in the swarm.
	res := planYAML(t, unfedHeader+`  bots:
    - id: mailer
      use: email-drive-file@1.1.0
      inputs:
        to: me@example.com
        file_id: "abc123"
  snaps: []
`)
	if len(res.Unfed) != 0 {
		t.Errorf("explicit inputs did not count: %v", res.Unfed)
	}

	// A snap from upstream.
	res = planYAML(t, unfedHeader+`  bots:
    - id: recap
      use: recap-emails-to-pdf@0.3.0
    - id: mailer
      use: email-drive-file@1.1.0
      inputs:
        to: me@example.com
  snaps:
    - from: recap.drive_file_id
      to: mailer.file_id
`)
	if len(res.Unfed) != 0 {
		t.Errorf("a snap did not count: %v", res.Unfed)
	}
	if !res.OK() {
		t.Errorf("a fully wired swarm did not plan OK: %+v", res.Unfed)
	}
}

// An optional input is allowed to be empty — that is what optional means,
// and half the catalog relies on it.
func TestAnOptionalInputNeedsNothing(t *testing.T) {
	res := planYAML(t, unfedHeader+`  bots:
    - id: triage
      use: inbox-triage@0.1.0
  snaps: []
`)
	for _, u := range res.Unfed {
		if u.Port == "instructions" {
			t.Errorf("an optional input was reported as unfed: %v", u)
		}
	}
}

// A webhook swarm's entry bot is *designed* to be fed by the trigger, and
// this build never delivers one. Same failure, different fix — so it gets
// a different message rather than "you forgot a snap".
func TestAWebhookSwarmSaysWhyItsEntryInputIsEmpty(t *testing.T) {
	res := planYAML(t, unfedHeader+`  trigger:
    type: webhook
    expr: "website.form.submitted"
  bots:
    - id: intake
      use: form-to-sheet@0.1.0
      inputs:
        sheet: Leads
  snaps: []
`)
	if len(res.Unfed) == 0 {
		t.Fatal("expected payload to be reported")
	}
	msg := res.Unfed[0].Error()
	for _, want := range []string{"payload", "webhook", "docs/scheduler.md"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message does not mention %q: %s", want, msg)
		}
	}
}

// Every swarm in the repo has to pass, or the check is describing a
// standard the catalog itself doesn't meet.
func TestEveryCatalogSwarmFillsItsRequiredInputs(t *testing.T) {
	root := repoRootForLevels(t)
	swarms, _ := filepath.Glob(filepath.Join(root, "examples", "swarms", "*.yaml"))
	if len(swarms) == 0 {
		t.Fatal("no swarms found")
	}
	for _, path := range swarms {
		res, err := Plan(path, filepath.Join(root, "bots"))
		if err != nil {
			t.Errorf("%s: %v", filepath.Base(path), err)
			continue
		}
		for _, u := range res.Unfed {
			t.Errorf("%s: %v", filepath.Base(path), u)
		}
	}
}

func contains(hay []string, needle string) bool {
	for _, h := range hay {
		if h == needle {
			return true
		}
	}
	return false
}

// A port that is both snapped into and given a value: the runner resolves
// explicit inputs first, so the snap is silently dropped. The diagram shows
// a wire, the plan type-checks it, and at run time it carries nothing.
//
// Found in a real composed draft that set notifier.message to a fixed
// string and snapped a summary into it. The plan said OK, and that Slack
// message would have been the fixed string forever.
func TestAPortCannotBeBothSnappedAndGivenAValue(t *testing.T) {
	res := planYAML(t, unfedHeader+`  bots:
    - id: recap
      use: recap-emails-to-pdf@0.3.0
    - id: mailer
      use: email-drive-file@1.1.0
      inputs:
        to: me@example.com
        file_id: "hardcoded"
  snaps:
    - from: recap.drive_file_id
      to: mailer.file_id
`)
	if res.OK() {
		t.Fatal("a swarm whose snap is silently dropped planned OK")
	}
	var found bool
	for _, e := range res.Invalid {
		if strings.Contains(e.Error(), "mailer.file_id") && strings.Contains(e.Error(), "silently dropped") {
			found = true
		}
	}
	if !found {
		t.Errorf("the collision was not reported: %v", res.Invalid)
	}

	// Either one alone is fine — this must not become "you may never set
	// an input on a bot that has any snap".
	res = planYAML(t, unfedHeader+`  bots:
    - id: recap
      use: recap-emails-to-pdf@0.3.0
    - id: mailer
      use: email-drive-file@1.1.0
      inputs:
        to: me@example.com
  snaps:
    - from: recap.drive_file_id
      to: mailer.file_id
`)
	if len(res.Invalid) != 0 {
		t.Errorf("a value on a different port was reported: %v", res.Invalid)
	}
}

// A retry re-runs the *entire bot*. A bot that sent an email and then
// failed on its last step will send that email again — so retry on a bot
// that writes somewhere is a duplicate-send waiting to happen, and almost
// certainly not what the author meant. Refused at plan time rather than
// discovered at 3am.
func TestRetryIsRefusedOnABotThatWrites(t *testing.T) {
	res := planYAML(t, unfedHeader+`  bots:
    - id: sender
      use: email-send-approved@0.1.0
      retry: 2
      inputs:
        draft_id: d1
        summary: s
  snaps: []
`)
	if res.OK() {
		t.Fatal("retry on a sending bot planned OK")
	}
	var found bool
	for _, e := range res.Invalid {
		if strings.Contains(e.Error(), "sender") && strings.Contains(e.Error(), "re-runs the whole bot") {
			found = true
		}
	}
	if !found {
		t.Errorf("not refused for the right reason: %v", res.Invalid)
	}
}

// A bot that only reads is exactly what retry is for.
func TestRetryIsFineOnABotThatOnlyReads(t *testing.T) {
	res := planYAML(t, unfedHeader+`  bots:
    - id: watch
      use: competitor-watch@0.1.0
      retry: 2
      inputs:
        urls: ["https://example.com"]
  snaps: []
`)
	for _, e := range res.Invalid {
		t.Errorf("a read-only bot was refused a retry: %v", e)
	}
}

func TestAnAbsurdRetryCountIsRefused(t *testing.T) {
	res := planYAML(t, unfedHeader+`  bots:
    - id: watch
      use: competitor-watch@0.1.0
      retry: 30
      inputs:
        urls: ["https://example.com"]
  snaps: []
`)
	if res.OK() {
		t.Fatal("retry: 30 planned OK")
	}
}
