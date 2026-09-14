package step

import (
	"errors"
	"github.com/redbotster/nanobots/internal/linkedin"
	"strings"
	"testing"
)

type fakeLinkedInAPI struct {
	urn     string
	urnErr  error
	gotURN  string
	gotText string
	id      string
	postErr error

	gotCommentURN string
	gotMax        int
	comments      []linkedin.Comment
	commentsErr   error
}

func (f *fakeLinkedInAPI) Comments(postURN string, max int) ([]linkedin.Comment, error) {
	f.gotCommentURN, f.gotMax = postURN, max
	return f.comments, f.commentsErr
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

func TestDispatchLinkedInCommentsListFlattensForAListJSONPort(t *testing.T) {
	f := &fakeLinkedInAPI{comments: []linkedin.Comment{
		{ID: "urn:li:comment:1", Text: "does this work for private pages?", Author: "urn:li:person:abc", At: "2026-09-14T10:00:00Z"},
	}}
	out, err := dispatchLinkedIn(f, "comments.list",
		map[string]any{"post_urn": "urn:li:share:7", "max_results": "5"})
	if err != nil {
		t.Fatalf("dispatchLinkedIn: %v", err)
	}
	if f.gotCommentURN != "urn:li:share:7" || f.gotMax != 5 {
		t.Errorf("urn=%q max=%d — params did not reach the client", f.gotCommentURN, f.gotMax)
	}
	first := out.(map[string]any)["comments"].([]any)[0].(map[string]any)
	if first["text"] != "does this work for private pages?" {
		t.Errorf("comment = %#v", first)
	}
}

// The 403 this returns is the normal state for an app without Community
// Management approval, so it must arrive as an explanation rather than a
// bare status code.
func TestLinkedInCommentsErrorReachesTheCaller(t *testing.T) {
	f := &fakeLinkedInAPI{commentsErr: errors.New("linkedin: reading comments needs the Community Management API product")}
	if _, err := dispatchLinkedIn(f, "comments.list", map[string]any{"post_urn": "urn:li:share:7"}); err == nil {
		t.Fatal("expected the client's error to propagate")
	} else if !strings.Contains(err.Error(), "Community Management") {
		t.Errorf("error lost its explanation: %v", err)
	}
}
