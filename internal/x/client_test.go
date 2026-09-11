package x

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
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
