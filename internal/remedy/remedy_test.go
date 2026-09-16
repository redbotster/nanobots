package remedy

import (
	"strings"
	"testing"
)

// Ported from web/src/lib/runError.test.ts, which is where these rules lived
// before the CLI needed them too. Same cases, same real errors — so the two
// callers of this table cannot quietly diverge on what a failure means.

func TestParseStripsTheTransportChain(t *testing.T) {
	// Verbatim from a real failed run on this machine.
	raw := `container exited 1: nanobot-agent: error: bot notify: step "send": ` +
		"callback /internal/steps/notify: 1Claw vault is locked: Passkey " +
		"verification required to access vault secrets. Unlock with your passkey and retry.\n"

	origin, msg := Parse(raw)
	if origin != "notify/send" {
		t.Errorf("origin = %q", origin)
	}
	want := "1Claw vault is locked: Passkey verification required to access vault secrets. " +
		"Unlock with your passkey and retry."
	if msg != want {
		t.Errorf("message = %q", msg)
	}
}

func TestParseHandlesTheDoubledContainerPrefix(t *testing.T) {
	raw := "container exited 1: container exited 1: nanobot-agent: error: " +
		`bot content-ideas: step "brainstorm": callback /internal/steps/ai_generate: ` +
		"shroud: chat failed (401)"

	origin, msg := Parse(raw)
	if origin != "content-ideas/brainstorm" {
		t.Errorf("origin = %q", origin)
	}
	if msg != "shroud: chat failed (401)" {
		t.Errorf("message = %q", msg)
	}
}

func TestParseKeepsAnErrorWithNoStepOrigin(t *testing.T) {
	origin, msg := Parse("swarm does not type-check: port mismatch")
	if origin != "" {
		t.Errorf("origin = %q, want empty", origin)
	}
	if msg != "swarm does not type-check: port mismatch" {
		t.Errorf("message = %q", msg)
	}
}

func TestParseKeepsOnlyTheFirstLine(t *testing.T) {
	if _, msg := Parse("first line\nsecond line\nthird"); msg != "first line" {
		t.Errorf("message = %q", msg)
	}
}

// The stripping must never be able to empty out an error — a blank reason
// would be strictly worse than the noisy one it replaced.
func TestParseNeverEmptiesANonEmptyError(t *testing.T) {
	for _, raw := range []string{
		"container exited 1:",
		`bot notify: step "send":`,
		"nanobot-agent: error:",
		"x",
	} {
		if _, msg := Parse(raw); msg == "" {
			t.Errorf("Parse(%q) emptied the message", raw)
		}
	}
}

func TestParseReturnsNothingForNothing(t *testing.T) {
	for _, raw := range []string{"", "   ", "\n\t "} {
		if _, msg := Parse(raw); msg != "" {
			t.Errorf("Parse(%q) = %q, want empty", raw, msg)
		}
	}
}

// The failure six of the catalog swarms actually hit.
func TestForOffersToConnectTheServiceAMissingCredentialNames(t *testing.T) {
	r := For(`container exited 1: nanobot-agent: error: bot notify: step "send": callback ` +
		`/internal/steps/notify: no connected account yet (oneclaw: request failed (404): ` +
		`{"detail":"Secret slack/bot_token not found"}) — connect it from Settings`)

	if r == nil || r.Action == nil {
		t.Fatalf("remedy = %+v, want a connect action", r)
	}
	if r.Action.Label != "Connect Slack" {
		t.Errorf("label = %q", r.Action.Label)
	}
	if r.Action.Page != "settings" {
		t.Errorf("page = %q", r.Action.Page)
	}
	if !strings.Contains(r.Advice, "Slack") {
		t.Errorf("advice = %q", r.Advice)
	}
}

func TestForNamesTheRightServiceNotJustAnyService(t *testing.T) {
	for _, tc := range []struct{ err, want string }{
		{"bot invoice-chaser: Secret stripe/api_key not found", "Connect Stripe"},
		{"bot github-issues-digest: Secret github/token not found", "Connect GitHub"},
		{"bot post-publisher: Secret linkedin/refresh_token not found", "Connect LinkedIn"},
	} {
		r := For(tc.err)
		if r == nil || r.Action == nil || r.Action.Label != tc.want {
			t.Errorf("For(%q) action = %+v, want %q", tc.err, r, tc.want)
		}
	}
}

// The vault fix is on a phone, so offering a button would be a lie about
// where the work happens.
func TestForExplainsTheVaultLockWithoutPretendingThereIsAButton(t *testing.T) {
	r := For("1Claw vault is locked: Passkey verification required to access vault secrets.")
	if r == nil {
		t.Fatal("no remedy")
	}
	if !strings.Contains(r.Advice, "passkey") {
		t.Errorf("advice = %q", r.Advice)
	}
	if r.Action != nil {
		t.Errorf("offered a button for a fix that happens on a phone: %+v", r.Action)
	}
}

// A declined approval is a decision. Someone debugging their own "no" is the
// failure mode here.
func TestForSaysADeclinedApprovalWasNotAFault(t *testing.T) {
	r := For(`bot email-send-approved: step "gate": not approved (decided_by=cli)`)
	if r == nil || !strings.Contains(r.Advice, "wasn't a fault") {
		t.Errorf("advice = %+v", r)
	}
}

func TestForDistinguishesAnUnattendedDeclineFromAHumanOne(t *testing.T) {
	r := For(`step "gate": not approved (decided_by=nobody — no terminal attached to ask)`)
	if r == nil || !strings.Contains(r.Advice, "Nothing was attached") {
		t.Errorf("advice = %+v", r)
	}
}

// The most common failure on a machine running scheduled swarms: 54 runs on
// the development machine, every one an approval nobody answered.
func TestForNamesAnUnansweredApprovalAsNobodyBeingThere(t *testing.T) {
	// Exactly what internal/runner produces. The message is the fact alone,
	// so this is the only place the advice lives.
	r := For(`nobody answered the approval "Send 'recap.pdf' to me@example.com?" within 30m0s`)
	if r == nil {
		t.Fatal("no remedy for the app's most common failure")
	}
	if !strings.Contains(r.Advice, "nobody answered in time") {
		t.Errorf("advice = %q", r.Advice)
	}
	// "Run it again" is the wrong advice here — the next scheduled run goes
	// unanswered exactly the same way. It must name a durable fix.
	if strings.Contains(r.Advice, "Run it again") {
		t.Errorf("advice tells you to retry a thing that will fail the same way: %q", r.Advice)
	}
	if !strings.Contains(r.Advice, "phone") && !strings.Contains(r.Advice, "approval off this step") {
		t.Errorf("advice names no durable fix: %q", r.Advice)
	}
	if r.Docs != "docs/approvals.md" {
		t.Errorf("docs = %q", r.Docs)
	}
}

func TestForDoesNotConfuseAnUnansweredApprovalWithADecline(t *testing.T) {
	declined := For("bot mailer: step approve: not approved")
	if declined == nil || !strings.Contains(declined.Advice, "declined") {
		t.Fatalf("advice = %+v", declined)
	}
	if strings.Contains(declined.Advice, "nobody answered in time") {
		t.Errorf("a decline was given the unanswered-approval advice: %q", declined.Advice)
	}
}

func TestForPointsAFanOutMismatchAtTheDoc(t *testing.T) {
	r := For("fan-out inputs disagree: triage.tickets.*.subject has 2 item(s) but an " +
		"earlier one has 1 — they iterate together, so they must be the same length")
	if r == nil || r.Docs != "docs/fan-out.md" {
		t.Errorf("remedy = %+v", r)
	}
}

func TestForRecognisesAProviderRateLimit(t *testing.T) {
	r := For("llm: https://generativelanguage.googleapis.com/... returned 429: quota exceeded")
	if r == nil || !strings.Contains(r.Advice, "rate-limiting") {
		t.Errorf("remedy = %+v", r)
	}
}

func TestForHandlesDockerAndTheMissingModel(t *testing.T) {
	if r := For("Cannot connect to the Docker daemon at unix:///var/run/docker.sock"); r == nil ||
		!strings.Contains(r.Advice, "Start Docker Desktop") {
		t.Errorf("docker remedy = %+v", r)
	}
	r := For("ai.generate: no LLM is configured")
	if r == nil || r.Action == nil || r.Action.Label != "Set up a model" {
		t.Errorf("llm remedy = %+v", r)
	}
}

// A confidently wrong suggestion sends someone to reconfigure a thing that
// was never the problem, which is worse than saying nothing.
func TestForOffersNothingForAFailureItDoesNotRecognise(t *testing.T) {
	for _, raw := range []string{
		"bot repurposer: model response was not valid JSON",
		"something nobody has seen before",
		"",
		"   ",
	} {
		if r := For(raw); r != nil {
			t.Errorf("For(%q) = %+v, want nil", raw, r)
		}
	}
}
