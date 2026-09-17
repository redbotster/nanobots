package webui

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// asset is one file out of the embedded build, held both ways.
type asset struct {
	body      []byte
	gz        []byte // nil when compressing did not pay
	etag      string
	mime      string
	immutable bool
}

// assets compresses and tags the whole embedded build, once.
//
// `http.FileServer` over an embed.FS does neither, and both showed up the
// same way — by looking at what the browser actually downloaded:
//
//	GET /assets/index-DKRS_W37.js -> 200 (352160B)
//
// 352KB of JavaScript, uncompressed, on every single load. gzip takes it to
// 110KB. And with no ETag and no Last-Modified (an embed.FS file has a zero
// modtime, so net/http emits neither), there was nothing for the browser to
// revalidate against either: a reload paid the full 352KB again.
//
// Vite content-hashes every filename under /assets, which is what makes the
// second half safe to state as strongly as `immutable`: that exact byte
// sequence is what that name means, forever, and a rebuild produces a
// different name. index.html is not hashed — it is the file that names the
// hashed ones — so it revalidates on every load instead.
//
// Done once at startup rather than per request. The whole build is about
// 400KB; holding a compressed copy beside it costs another 120KB of process
// memory and removes a compression pass from every asset request.
func buildAssets(sub fs.FS) map[string]*asset {
	out := map[string]*asset{}
	_ = fs.WalkDir(sub, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		body, err := fs.ReadFile(sub, name)
		if err != nil {
			return nil
		}
		sum := sha256.Sum256(body)
		a := &asset{
			body:      body,
			etag:      `"` + hex.EncodeToString(sum[:16]) + `"`,
			mime:      mimeOf(name),
			immutable: strings.HasPrefix(name, "assets/"),
		}
		var buf bytes.Buffer
		zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
		if _, err := zw.Write(body); err == nil && zw.Close() == nil {
			// Only when it actually saves something. A .png or a tiny file
			// comes out larger, and serving that would be a cost dressed up
			// as a saving.
			if buf.Len() < len(body)*9/10 {
				a.gz = append([]byte(nil), buf.Bytes()...)
			}
		}
		out[name] = a
		return nil
	})
	return out
}

func mimeOf(name string) string {
	switch {
	case strings.HasSuffix(name, ".html"):
		return "text/html; charset=utf-8"
	case strings.HasSuffix(name, ".js"):
		return "text/javascript; charset=utf-8"
	case strings.HasSuffix(name, ".css"):
		return "text/css; charset=utf-8"
	case strings.HasSuffix(name, ".json"):
		return "application/json"
	case strings.HasSuffix(name, ".svg"):
		return "image/svg+xml"
	case strings.HasSuffix(name, ".png"):
		return "image/png"
	case strings.HasSuffix(name, ".woff2"):
		return "font/woff2"
	case strings.HasSuffix(name, ".ico"):
		return "image/x-icon"
	}
	return "application/octet-stream"
}

// serve writes one asset, negotiating gzip and answering 304 where it can.
func (a *asset) serve(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("Content-Type", a.mime)
	h.Set("ETag", a.etag)
	if a.immutable {
		// A year, and immutable so a browser does not even revalidate.
		// Safe only because the name carries the content hash.
		h.Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		// index.html names the hashed files, so a stale one is a whole
		// stale app. It revalidates every load and costs an ETag round
		// trip to do it.
		h.Set("Cache-Control", "no-cache")
	}

	if match := r.Header.Get("If-None-Match"); match != "" && etagIn(match, a.etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	// Range and Content-Encoding together is a trap: the range would be into
	// the compressed bytes, which is not what the client asked for. Nothing
	// in this app range-requests its own assets, but a media element or a
	// download manager might, so the identity copy is what answers those.
	if a.gz != nil && r.Header.Get("Range") == "" && acceptsGzip(r) {
		h.Set("Content-Encoding", "gzip")
		h.Set("Vary", "Accept-Encoding")
		h.Set("Content-Length", strconv.Itoa(len(a.gz)))
		if r.Method == http.MethodHead {
			return
		}
		_, _ = w.Write(a.gz)
		return
	}
	h.Set("Vary", "Accept-Encoding")
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(a.body))
}

func acceptsGzip(r *http.Request) bool {
	for _, part := range strings.Split(r.Header.Get("Accept-Encoding"), ",") {
		name, _, _ := strings.Cut(strings.TrimSpace(part), ";")
		if strings.EqualFold(name, "gzip") {
			return true
		}
	}
	return false
}

// etagIn is If-None-Match's list form. A browser sends back exactly what it
// was given, but a proxy may send several.
func etagIn(header, etag string) bool {
	if strings.TrimSpace(header) == "*" {
		return true
	}
	for _, part := range strings.Split(header, ",") {
		if strings.TrimPrefix(strings.TrimSpace(part), "W/") == etag {
			return true
		}
	}
	return false
}

// once, so a second Handler() call (tests build several) does not gzip the
// whole build again.
var (
	assetsOnce sync.Once
	assetsMap  map[string]*asset
)

func loadedAssets(sub fs.FS) map[string]*asset {
	assetsOnce.Do(func() { assetsMap = buildAssets(sub) })
	return assetsMap
}
