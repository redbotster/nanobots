package step

import (
	"errors"
	"testing"
)

type fakeLinkedInAPI struct {
	urn     string
	urnErr  error
	gotURN  string
	gotText string
	id      string
	postErr error
}

func (f *fakeLinkedInAPI) UserInfo() (string, error) { return f.urn, f.urnErr }

func (f *fakeLinkedInAPI) PostShare(personURN, text string) (string, error) {
	f.gotURN, f.gotText = personURN, text
	return f.id, f.postErr
}

func TestDispatchLinkedInPostsPublishResolvesURNThenPosts(t *testing.T) {
	f := &fakeLinkedInAPI{urn: "urn:li:person:abc", id: "share-1"}
	out, err := dispatchLinkedIn(f, "posts.publish", map[string]any{"text": "hello"})
	if err != nil {
		t.Fatalf("dispatchLinkedIn: %v", err)
	}
	if f.gotURN != "urn:li:person:abc" || f.gotText != "hello" {
		t.Errorf("fake got urn=%q text=%q", f.gotURN, f.gotText)
	}
	m, ok := out.(map[string]any)
	if !ok || m["id"] != "share-1" {
		t.Errorf("out = %#v", out)
	}
}

func TestDispatchLinkedInPropagatesUserInfoError(t *testing.T) {
	f := &fakeLinkedInAPI{urnErr: errors.New("boom")}
	if _, err := dispatchLinkedIn(f, "posts.publish", map[string]any{"text": "hi"}); err == nil {
		t.Fatal("expected UserInfo error to propagate")
	}
}

func TestDispatchLinkedInPropagatesPostError(t *testing.T) {
	f := &fakeLinkedInAPI{urn: "urn:li:person:abc", postErr: errors.New("boom")}
	if _, err := dispatchLinkedIn(f, "posts.publish", map[string]any{"text": "hi"}); err == nil {
		t.Fatal("expected PostShare error to propagate")
	}
}

func TestDispatchLinkedInUnsupportedOpErrors(t *testing.T) {
	if _, err := dispatchLinkedIn(&fakeLinkedInAPI{}, "nonsense.op", nil); err == nil {
		t.Fatal("expected an error for an unrecognized op")
	}
}

func TestLinkedInClientErrorsWhenNotConfigured(t *testing.T) {
	l := &LiveDeps{}
	if _, err := l.linkedinClient(); err == nil {
		t.Fatal("expected an error when LinkedInConfig is unset")
	}
}
