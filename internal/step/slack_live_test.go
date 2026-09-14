package step

import "testing"

func TestSlackChannelFromRecognizesPrefix(t *testing.T) {
	target, ok := slackChannelFrom("slack:#general")
	if !ok || target != "#general" {
		t.Errorf("target=%q ok=%v, want #general, true", target, ok)
	}
}

func TestSlackChannelFromRejectsOtherPrefixes(t *testing.T) {
	for _, channel := range []string{"email:me@example.com", "sms:+15551234567", "#general", ""} {
		if _, ok := slackChannelFrom(channel); ok {
			t.Errorf("slackChannelFrom(%q) = ok, want not-Slack", channel)
		}
	}
}

func TestSlackClientErrorsWhenNotConfigured(t *testing.T) {
	l := &LiveDeps{}
	if _, err := l.slackClient(); err == nil {
		t.Fatal("expected an error when SlackConfig is unset")
	}
}

func TestNotifyFallsBackToDemoForNonSlackChannels(t *testing.T) {
	l := &LiveDeps{Demo: NewDemoDeps(t.TempDir(), nil)}
	if err := l.Notify("hi", "email:me@example.com"); err != nil {
		t.Fatalf("Notify: %v, want the demo no-op to succeed", err)
	}
}

func TestNotifyErrorsForSlackChannelWhenNotConfigured(t *testing.T) {
	l := &LiveDeps{Demo: NewDemoDeps(t.TempDir(), nil)}
	if err := l.Notify("hi", "slack:#general"); err == nil {
		t.Fatal("expected an error — Slack isn't configured, so this must not silently no-op")
	}
}

type fakeSlackAPI struct {
	gotChannel, gotText string
	ts                  string
	err                 error
}

func (f *fakeSlackAPI) PostMessage(channel, text string) (string, error) {
	f.gotChannel, f.gotText = channel, text
	return f.ts, f.err
}

// lead-router calls Slack through service.call, not through `notify`. That
// path had no dispatcher, so connecting Slack in Settings — which the app
// offers — still left the bot failing with "no direct slack integration".
func TestDispatchSlackPostsToTheChannel(t *testing.T) {
	f := &fakeSlackAPI{ts: "1726000000.123"}
	out, err := dispatchSlack(f, "messages.post", map[string]any{
		"channel": "#sales", "text": "New lead routed: Dana",
	})
	if err != nil {
		t.Fatalf("dispatchSlack: %v", err)
	}
	if f.gotChannel != "#sales" || f.gotText != "New lead routed: Dana" {
		t.Errorf("channel=%q text=%q", f.gotChannel, f.gotText)
	}
	m := out.(map[string]any)
	if m["ts"] != "1726000000.123" || m["channel"] != "#sales" {
		t.Errorf("out = %#v", m)
	}
}

// Slack silently accepts an empty channel in some shapes and drops the
// message; refusing beats a post nobody receives.
func TestDispatchSlackRefusesAnEmptyChannel(t *testing.T) {
	if _, err := dispatchSlack(&fakeSlackAPI{}, "messages.post", map[string]any{"text": "hi"}); err == nil {
		t.Fatal("expected an error for a missing channel")
	}
}

func TestDispatchSlackRejectsAnUnknownOp(t *testing.T) {
	if _, err := dispatchSlack(&fakeSlackAPI{}, "channels.archive", nil); err == nil {
		t.Fatal("expected an error for an unsupported op")
	}
}
