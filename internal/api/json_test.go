package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// The whole point: a caller that already has this body gets told so, with
// no body. GET /api/runs is 91KB and polled every two seconds.
func TestConditionalGETAnswers304ForAnUnchangedBody(t *testing.T) {
	payload := []map[string]any{{"id": "a"}, {"id": "b"}}
	handler := func(w http.ResponseWriter, r *http.Request) {
		writeJSONCached(w, r, http.StatusOK, payload)
	}

	first := httptest.NewRecorder()
	handler(first, httptest.NewRequest(http.MethodGet, "/x", nil))
	if first.Code != http.StatusOK {
		t.Fatalf("first code = %d", first.Code)
	}
	etag := first.Header().Get("ETag")
	if etag == "" {
		t.Fatal("no ETag, so a client can never revalidate")
	}
	if first.Body.Len() == 0 {
		t.Fatal("first response had no body")
	}

	second := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	r.Header.Set("If-None-Match", etag)
	handler(second, r)
	if second.Code != http.StatusNotModified {
		t.Fatalf("second code = %d, want 304", second.Code)
	}
	if second.Body.Len() != 0 {
		t.Errorf("304 carried a %d-byte body", second.Body.Len())
	}
}

// A changed body must not be answered from a stale ETag, or the UI freezes
// on old data — a far worse bug than the bandwidth this saves.
func TestConditionalGETSendsTheBodyWhenItChanges(t *testing.T) {
	rec := httptest.NewRecorder()
	writeJSONCached(rec, httptest.NewRequest(http.MethodGet, "/x", nil), http.StatusOK,
		[]string{"one"})
	etag := rec.Header().Get("ETag")

	changed := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	r.Header.Set("If-None-Match", etag)
	writeJSONCached(changed, r, http.StatusOK, []string{"one", "two"})
	if changed.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200 — the data changed", changed.Code)
	}
	if changed.Header().Get("ETag") == etag {
		t.Error("the ETag did not change with the body")
	}
}

// A proxy may rewrite the tag as weak, or send several. Getting either
// wrong silently disables the whole thing rather than breaking loudly.
func TestConditionalGETHandlesWeakAndListedETags(t *testing.T) {
	rec := httptest.NewRecorder()
	writeJSONCached(rec, httptest.NewRequest(http.MethodGet, "/x", nil), http.StatusOK, []int{1})
	etag := rec.Header().Get("ETag")

	for _, header := range []string{etag, "W/" + etag, `"other", ` + etag, "*"} {
		got := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/x", nil)
		r.Header.Set("If-None-Match", header)
		writeJSONCached(got, r, http.StatusOK, []int{1})
		if got.Code != http.StatusNotModified {
			t.Errorf("If-None-Match %q => %d, want 304", header, got.Code)
		}
	}
}

// no-store, not no-cache: with no-cache the browser can satisfy the
// revalidation itself and hand JavaScript a 200 with a body, which saves
// the bytes but not the parse or the re-render.
func TestConditionalGETKeepsTheBrowserCacheOutOfIt(t *testing.T) {
	rec := httptest.NewRecorder()
	writeJSONCached(rec, httptest.NewRequest(http.MethodGet, "/x", nil), http.StatusOK, []int{1})
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
}
