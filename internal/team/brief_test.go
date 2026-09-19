package team

import (
	"strings"
	"testing"
)

// The brief has to say three things or it isn't doing its job: which
// branch/workspace this is, what the actual task is, and — the one this
// package exists to get right — that nothing here bypasses the ordinary
// approval gate.
func TestBuildBriefStatesTheRoleTaskAndSafetyModel(t *testing.T) {
	brief := BuildBrief(TaskInput{Role: "backend-engineer", Task: "add a stripe-watch bot"}, "/usr/local/bin/nanobots")

	for _, want := range []string{
		"backend-engineer",
		"team/backend-engineer",
		"add a stripe-watch bot",
		"/usr/local/bin/nanobots",
		"nothing you write or edit here takes effect against a real account",
		"not more constrained than a person contributing here",
	} {
		if !strings.Contains(brief, want) {
			t.Errorf("brief missing %q\n---\n%s", want, brief)
		}
	}
}

func TestBuildBriefDoesNotFenceTheAgentToOneDirectory(t *testing.T) {
	// Unlike the foundry's brief (one hard boundary: bots/<new-id>/ only),
	// a Team brief must not claim a restriction that doesn't exist — its
	// safety comes from the approval gate, not a filesystem fence.
	brief := BuildBrief(TaskInput{Role: "designer", Task: "anything"}, "/bin/nanobots")
	if strings.Contains(brief, "HARD BOUNDARY") {
		t.Error("a Team brief claims a hard filesystem boundary it doesn't enforce")
	}
}
