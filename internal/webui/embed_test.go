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

// findAsset returns one hashed file out of the embedded build, or "".
func findAsset(t *testing.T, suffix string) string {
	t.Helper()
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		t.Fatal(err)
	}
	var found string
	_ = fs.WalkDir(sub, ".", func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, suffix) && found == "" {
			found = p
		}
		return nil
	})
	return found
}

// The bundle is 352KB and was served uncompressed, on every load, because
// http.FileServer over an embed.FS does not compress. gzip takes it to
// 110KB, which is more than every API saving in this repo put together.
func TestTheBundleIsCompressedWhenTheBrowserSaysItCanBe(t *testing.T) {
	if !Available() {
		t.Skip("no UI in this binary; run `make ui` to exercise the serving path")
	}
	name := findAsset(t, ".js")
	if name == "" {
		t.Skip("no js asset in this build")
	}
	h := Handler()

	req := httptest.NewRequest(http.MethodGet, "/"+name, nil)
	req.Header.Set("Accept-Encoding", "gzip, deflate, br")
	gz := httptest.NewRecorder()
	h.ServeHTTP(gz, req)

	plain := httptest.NewRecorder()
	h.ServeHTTP(plain, httptest.NewRequest(http.MethodGet, "/"+name, nil))

	if plain.Header().Get("Content-Encoding") != "" {
		t.Error("a client that asked for no encoding was sent one anyway")
	}
	if gz.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", gz.Header().Get("Content-Encoding"))
	}
	if gz.Body.Len() >= plain.Body.Len() {
		t.Errorf("the compressed copy is %d bytes against %d — no saving at all",
			gz.Body.Len(), plain.Body.Len())
	}
	// Vary matters on both: a cache that does not know the body depends on
	// Accept-Encoding will hand gzip bytes to a client that cannot read them.
	for _, rec := range []*httptest.ResponseRecorder{gz, plain} {
		if rec.Header().Get("Vary") != "Accept-Encoding" {
			t.Error("no Vary: Accept-Encoding, so a shared cache can serve the wrong copy")
		}
	}
}

// A ranged request must get the identity copy: a range into the compressed
// bytes is not the range the client asked for.
func TestARangedRequestIsNotCompressed(t *testing.T) {
	if !Available() {
		t.Skip("no UI in this binary")
	}
	name := findAsset(t, ".js")
	if name == "" {
		t.Skip("no js asset in this build")
	}
	req := httptest.NewRequest(http.MethodGet, "/"+name, nil)
	req.Header.Set("Accept-Encoding", "gzip")
	req.Header.Set("Range", "bytes=0-9")
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, req)

	if rec.Header().Get("Content-Encoding") == "gzip" {
		t.Error("a ranged request was answered with compressed bytes")
	}
	if rec.Code != http.StatusPartialContent {
		t.Errorf("status = %d, want 206", rec.Code)
	}
}

// Every asset name Vite emits carries a content hash, so the bytes behind
// that name never change and a year-long immutable cache is the truth.
// index.html is the file that names the hashed ones, so it must not be
// cached that way — a stale one is a whole stale app pointing at bundles
// that no longer exist.
func TestHashedAssetsAreImmutableAndTheShellIsNot(t *testing.T) {
	if !Available() {
		t.Skip("no UI in this binary")
	}
	h := Handler()

	if name := findAsset(t, ".js"); name != "" {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/"+name, nil))
		if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
			t.Errorf("Cache-Control on a content-hashed asset = %q", cc)
		}
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if cc := rec.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("Cache-Control on the app shell = %q, want no-cache", cc)
	}
}

// An embed.FS file has a zero modtime, so net/http emits neither
// Last-Modified nor an ETag and a reload pays full price for everything.
func TestAReloadRevalidatesInsteadOfRedownloading(t *testing.T) {
	if !Available() {
		t.Skip("no UI in this binary")
	}
	h := Handler()
	first := httptest.NewRecorder()
	h.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/", nil))

	etag := first.Header().Get("ETag")
	if etag == "" {
		t.Fatal("no ETag, so there is nothing for a browser to revalidate against")
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("If-None-Match", etag)
	second := httptest.NewRecorder()
	h.ServeHTTP(second, req)

	if second.Code != http.StatusNotModified {
		t.Errorf("status = %d, want 304", second.Code)
	}
	if second.Body.Len() != 0 {
		t.Errorf("a 304 carried %d bytes of body", second.Body.Len())
	}
}
