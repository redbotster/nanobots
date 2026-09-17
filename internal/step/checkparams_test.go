package step

import (
	"strings"
	"testing"
)

// The measured case: a swarm snapped a lead record into a bot that reads
// `thread.from`, so `to:` resolved to nothing and the bot drafted an email
// with no recipient — then asked a human to approve sending it.
func TestADraftWithNoRecipientIsRefused(t *testing.T) {
	err := checkParams("gmail", "drafts.create", map[string]any{
		"drafts": []any{
			map[string]any{"to": "", "subject": "Re: ", "body": "three times that work"},
		},
	})
	if err == nil {
		t.Fatal("a draft with no recipient was accepted")
	}
	// The message has to point at the wiring, not at the bot: the bot is
	// doing exactly what it was told, with a field that arrived empty.
	if got := err.Error(); !strings.Contains(got, "no recipient") || !strings.Contains(got, "snapped") {
		t.Errorf("error does not say what went wrong or where to look: %q", got)
	}
}

func TestARealDraftIsLeftAlone(t *testing.T) {
	for name, params := range map[string]map[string]any{
		"one good draft": {"drafts": []any{
			map[string]any{"to": "dana@example.com", "subject": "Times", "body": "b"},
		}},
		// A bot that drafts nothing this run — an inbox with nothing urgent
		// in it — must not be turned into a failure.
		"no drafts at all": {"drafts": []any{}},
		// Anything that is not the shape this knows about is not this
		// function's business. Guessing here is how a check like this starts
		// refusing calls that work.
		"not a list": {"drafts": "later"},
		"no params":  nil,
	} {
		if err := checkParams("gmail", "drafts.create", params); err != nil {
			t.Errorf("%s was refused: %v", name, err)
		}
	}
}

// Every other op goes through untouched. This is a list of known failures,
// not a validation layer.
func TestOpsThisKnowsNothingAboutPassThrough(t *testing.T) {
	for _, op := range []string{"messages.list", "drafts.send", "files.get", "contacts.upsert"} {
		if err := checkParams("gmail", op, map[string]any{"anything": ""}); err != nil {
			t.Errorf("%s was refused: %v", op, err)
		}
	}
}
