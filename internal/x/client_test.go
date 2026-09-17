package x

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPostTweetSendsAuthAndBodyThenParsesID(t *testing.T) {
	var gotAuth, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]string{"id": "tweet-123", "text": "hello"},
		})
	}))
	defer srv.Close()

	c := NewClient("token-abc")
	c.BaseURL = srv.URL

	id, err := c.PostTweet("hello world")
	if err != nil {
		t.Fatalf("PostTweet: %v", err)
	}
	if id != "tweet-123" {
		t.Errorf("id = %q, want tweet-123", id)
	}
	if gotAuth != "Bearer token-abc" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if gotBody != `{"text":"hello world"}` {
		t.Errorf("body = %q", gotBody)
	}
}

func TestPostTweetSurfacesNonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"title":"Unauthorized"}`))
	}))
	defer srv.Close()

	c := NewClient("bad-token")
	c.BaseURL = srv.URL
	if _, err := c.PostTweet("hi"); err == nil {
		t.Fatal("expected an error on a 401 response")
	}
}

func TestPostTweetSurfacesMissingID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{"data": map[string]string{}})
	}))
	defer srv.Close()

	c := NewClient("token")
	c.BaseURL = srv.URL
	if _, err := c.PostTweet("hi"); err == nil {
		t.Fatal("expected an error when the response has no tweet id")
	}
}

// The mentions read, against the shape X's v2 API actually returns:
// data + includes.users for the handles + meta.newest_id for the bookmark
// (developer.x.com, GET /2/users/:id/mentions).
//
// The bookmark is the point. Every read is billed since X removed the free
// tier in February 2026, and a watch with no bookmark asks for the same
// window every run and pays for it every time.
func TestMentionsReadsTheBookmarkAndStitchesHandles(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/users/me" {
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]string{"id": "u1"}})
			return
		}
		gotQuery = r.URL.RawQuery
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]string{
				{"id": "1803", "text": "newest", "author_id": "42"},
				{"id": "1801", "text": "older", "author_id": "42"},
			},
			"includes": map[string]any{
				"users": []map[string]string{{"id": "42", "username": "priya"}},
			},
			"meta": map[string]string{"newest_id": "1803", "oldest_id": "1801"},
		})
	}))
	defer srv.Close()

	c := NewClient("tok")
	c.BaseURL = srv.URL

	page, err := c.Mentions("1799", 25)
	if err != nil {
		t.Fatalf("Mentions: %v", err)
	}
	if page.NewestID != "1803" {
		t.Errorf("NewestID = %q, want X's own meta.newest_id", page.NewestID)
	}
	if len(page.Mentions) != 2 || page.Mentions[0].Author != "@priya" {
		t.Errorf("mentions = %+v", page.Mentions)
	}
	if !strings.Contains(gotQuery, "since_id=1799") {
		t.Errorf("query = %q, want the bookmark sent so X bills only what is new", gotQuery)
	}
}

// A response without meta must not leave a watch with an empty bookmark:
// that is how it goes back to re-reading, and re-paying for, the same
// window every run. The list is newest first.
func TestMentionsFallsBackToTheNewestPostWhenMetaIsAbsent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/users/me" {
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]string{"id": "u1"}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]string{{"id": "1803", "text": "newest"}},
		})
	}))
	defer srv.Close()

	c := NewClient("tok")
	c.BaseURL = srv.URL
	page, err := c.Mentions("", 10)
	if err != nil {
		t.Fatalf("Mentions: %v", err)
	}
	if page.NewestID != "1803" {
		t.Errorf("NewestID = %q, want the newest post's id", page.NewestID)
	}
}

// Nothing new is the common case on a weekday cron, and it must come back
// as an empty page rather than as an error.
func TestMentionsWithNothingNew(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/users/me" {
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]string{"id": "u1"}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"meta": map[string]any{"result_count": 0}})
	}))
	defer srv.Close()

	c := NewClient("tok")
	c.BaseURL = srv.URL
	page, err := c.Mentions("1803", 10)
	if err != nil {
		t.Fatalf("Mentions: %v", err)
	}
	if len(page.Mentions) != 0 || page.NewestID != "" {
		t.Errorf("page = %+v, want nothing and no new bookmark", page)
	}
}
