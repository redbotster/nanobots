package step

import (
	"errors"
	"testing"
)

type fakeXAPI struct {
	gotText string
	id      string
	err     error
}

func (f *fakeXAPI) PostTweet(text string) (string, error) {
	f.gotText = text
	return f.id, f.err
}

func TestDispatchXPostsPublishSendsTextAndReturnsID(t *testing.T) {
	f := &fakeXAPI{id: "tweet-1"}
	out, err := dispatchX(f, "posts.publish", map[string]any{"text": "hello"})
	if err != nil {
		t.Fatalf("dispatchX: %v", err)
	}
	if f.gotText != "hello" {
		t.Errorf("gotText = %q", f.gotText)
	}
	m, ok := out.(map[string]any)
	if !ok || m["id"] != "tweet-1" {
		t.Errorf("out = %#v", out)
	}
}

func TestDispatchXPropagatesError(t *testing.T) {
	f := &fakeXAPI{err: errors.New("boom")}
	if _, err := dispatchX(f, "posts.publish", map[string]any{"text": "hi"}); err == nil {
		t.Fatal("expected error to propagate")
	}
}

func TestDispatchXUnsupportedOpErrors(t *testing.T) {
	if _, err := dispatchX(&fakeXAPI{}, "nonsense.op", nil); err == nil {
		t.Fatal("expected an error for an unrecognized op")
	}
}

func TestXClientErrorsWhenNotConfigured(t *testing.T) {
	l := &LiveDeps{}
	if _, err := l.xClient(); err == nil {
		t.Fatal("expected an error when XConfig is unset")
	}
}
