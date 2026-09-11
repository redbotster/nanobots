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
