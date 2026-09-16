package webui

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A binary built without `make ui` must say so, not serve a blank page or a
// bare 404 that reads as a broken install.
func TestWithoutABuiltUIItSaysSo(t *testing.T) {
	if Available() {
		t.Skip("this binary has a UI built in; the not-built path is what this covers")
	}
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503 — the API works, the UI is absent", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"not built into this binary", "npm run dev", "make ui"} {
		if !strings.Contains(body, want) {
			t.Errorf("the page does not mention %q, so it does not say what to do:\n%s", want, body)
		}
	}
}

// With a UI built in, a path that is not a file is the app's own route and
// must reach index.html rather than 404. Deep-linking /runs is the whole
// reason this fallback exists.
func TestADeepLinkReachesTheApp(t *testing.T) {
	if !Available() {
		t.Skip("no UI in this binary; run `make ui` to exercise the serving path")
	}
	h := Handler()
	for _, path := range []string{"/", "/runs", "/settings", "/some/deep/link"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200 from the SPA fallback", path, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "<div id=\"root\"") &&
			!strings.Contains(rec.Body.String(), "<script") {
			t.Errorf("GET %s did not return the app shell", path)
		}
	}
}

// A real asset must be served as itself, not swallowed by the fallback —
// otherwise every script tag would return index.html and the app would not
// boot. Checked against the actual hashed bundle rather than a guessed
// name, since Vite renames it on every build.
//
// Deliberately not /index.html: http.FileServer canonicalises that to "/"
// with a 301, which is correct and is not what this test is about.
func TestARealAssetIsNotSwallowedByTheFallback(t *testing.T) {
	if !Available() {
		t.Skip("no UI in this binary")
	}
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		t.Fatal(err)
	}
	var asset string
	_ = fs.WalkDir(sub, ".", func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".js") && asset == "" {
			asset = p
		}
		return nil
	})
	if asset == "" {
		t.Skip("no js asset in this build")
	}

	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/"+asset, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /%s = %d, want the asset itself", asset, rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "javascript") {
		t.Errorf("GET /%s served %s — the fallback swallowed a real asset", asset, ct)
	}
}
