package lab

import (
	"strings"
	"testing"
)

func TestParseDecisionAnswer(t *testing.T) {
	d, err := parseDecision(`{"action":"answer","text":"hi there"}`)
	if err != nil {
		t.Fatalf("parseDecision: %v", err)
	}
	if d.Action != actionAnswer || d.Text != "hi there" {
		t.Errorf("d = %+v", d)
	}
}

func TestParseDecisionDelegate(t *testing.T) {
	d, err := parseDecision(`{"action":"delegate","role":"backend-engineer","task":"add a bot"}`)
	if err != nil {
		t.Fatalf("parseDecision: %v", err)
	}
	if d.Action != actionDelegate || d.Role != "backend-engineer" || d.Task != "add a bot" {
		t.Errorf("d = %+v", d)
	}
}

func TestParseDecisionStrpsMarkdownFence(t *testing.T) {
	d, err := parseDecision("```json\n" + `{"action":"answer","text":"hi"}` + "\n```")
	if err != nil {
		t.Fatalf("parseDecision: %v", err)
	}
	if d.Text != "hi" {
		t.Errorf("d = %+v", d)
	}
}

func TestParseDecisionRejectsBadJSON(t *testing.T) {
	if _, err := parseDecision("not json"); err == nil {
		t.Fatal("expected an error for unparseable JSON")
	}
}

func TestParseDecisionRejectsAnUnknownAction(t *testing.T) {
	if _, err := parseDecision(`{"action":"launch_missiles"}`); err == nil {
		t.Fatal("expected an error for an unrecognized action")
	}
}

func TestParseDecisionRejectsDelegateMissingRole(t *testing.T) {
	if _, err := parseDecision(`{"action":"delegate","task":"do a thing"}`); err == nil {
		t.Fatal("expected an error for a delegate decision with no role")
	}
}

func TestParseDecisionRejectsStatusMissingRole(t *testing.T) {
	if _, err := parseDecision(`{"action":"status"}`); err == nil {
		t.Fatal("expected an error for a status decision with no role")
	}
}

func TestParseDecisionRejectsAnswerMissingText(t *testing.T) {
	if _, err := parseDecision(`{"action":"answer"}`); err == nil {
		t.Fatal("expected an error for an answer decision with no text")
	}
}

func TestRoutePromptListsExistingRoles(t *testing.T) {
	p := routePrompt([]turn{{who: "human", text: "what's up"}}, []string{"backend-engineer", "designer"})
	if !strings.Contains(p, "backend-engineer, designer") {
		t.Errorf("prompt doesn't list existing roles:\n%s", p)
	}
	if strings.Contains(p, "No roles exist yet") {
		t.Errorf("prompt claims no roles exist when some were passed")
	}
}

func TestRoutePromptSaysNoRolesYetWhenThereAreNone(t *testing.T) {
	p := routePrompt([]turn{{who: "human", text: "hi"}}, nil)
	if !strings.Contains(p, "No roles exist yet") {
		t.Errorf("prompt doesn't say no roles exist:\n%s", p)
	}
}

func TestRoutePromptIncludesThePriorConversation(t *testing.T) {
	p := routePrompt([]turn{
		{who: "human", text: "hello"},
		{who: "lab", text: "hi, what can I help with?"},
		{who: "human", text: "add a stripe bot"},
	}, nil)
	if !strings.Contains(p, "hi, what can I help with?") {
		t.Errorf("prompt drops earlier turns:\n%s", p)
	}
	if !strings.Contains(p, `The human just said: "add a stripe bot"`) {
		t.Errorf("prompt doesn't isolate the latest message:\n%s", p)
	}
}
