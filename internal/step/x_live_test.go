package step

import (
	"errors"
	"testing"

	"github.com/redbotster/nanobots/internal/x"
)

type fakeXAPI struct {
	gotText  string
	id       string
	err      error
	gotSince string
	gotMax   int
	mentions []x.Mention
}

func (f *fakeXAPI) Mentions(sinceID string, max int) ([]x.Mention, error) {
	f.gotSince, f.gotMax = sinceID, max
	return f.mentions, f.err
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

func TestDispatchXMentionsListFlattensForAListJSONPort(t *testing.T) {
	f := &fakeXAPI{mentions: []x.Mention{
		{ID: "1", Text: "nice post", Author: "@dana", Created: "2026-09-14T10:00:00Z"},
	}}
	out, err := dispatchX(f, "mentions.list", map[string]any{"since_id": "99", "max_results": "25"})
	if err != nil {
		t.Fatalf("dispatchX: %v", err)
	}
	if f.gotSince != "99" || f.gotMax != 25 {
		t.Errorf("since=%q max=%d — the params did not reach the client", f.gotSince, f.gotMax)
	}
	m := out.(map[string]any)
	if m["count"] != 1 {
		t.Errorf("count = %v", m["count"])
	}
	// Plain maps, not structs: this crosses into the interpreter as JSON and
	// a bot's list<json> port reads the fields by name.
	first := m["mentions"].([]any)[0].(map[string]any)
	if first["author"] != "@dana" || first["text"] != "nice post" {
		t.Errorf("mention = %#v", first)
	}
}

// max_results arrives as a string from a bot's `string` input port and as a
// number from a snap. Both have to work, and a malformed one must not fail
// a run that was otherwise fine.
func TestAtoiParamAcceptsBothShapesAndFallsBack(t *testing.T) {
	for _, tc := range []struct {
		in   any
		want int
	}{
		{"25", 25}, {float64(30), 30}, {7, 7}, {" 12 ", 12},
		{"", 10}, {"lots", 10}, {nil, 10},
	} {
		if got := atoiParam(tc.in, 10); got != tc.want {
			t.Errorf("atoiParam(%#v) = %d, want %d", tc.in, got, tc.want)
		}
	}
}
