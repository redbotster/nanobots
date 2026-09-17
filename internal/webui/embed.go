// Package webui serves the built WebUI out of the binary, so `nanobots up`
// is one command and one port instead of a daemon here and a Vite dev
// server there.
//
// The quick start used to be two terminals, and that is a real cost: it is
// the first thing a new user does and the first place they can get it
// wrong. A single binary also makes Homebrew, a GitHub release and an npx
// shim possible, none of which can ask someone to run npm first.
package webui

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// dist is filled by `make ui` (or GoReleaser), which copies web/dist here
// after `npm run build`.
//
// Only .gitkeep is committed. web/dist is a build artifact and does not
// belong in git, but //go:embed needs the directory to exist at compile
// time on a fresh clone — hence the placeholder, and hence Available()
// below rather than assuming index.html is there.
//
//go:embed all:dist
var dist embed.FS

// Available reports whether a real UI was built into this binary.
//
// False for a plain `go build` with no `make ui` first, which is the normal
// developer loop: they run the Vite dev server for hot reload. Callers use
// this to say so out loud rather than serving a blank page.
func Available() bool {
	f, err := dist.Open("dist/index.html")
	if err != nil {
		return false
	}
	_ = f.Close()
	return true
}

// Handler serves the built UI, falling back to index.html for any path that
// is not a file.
//
// The fallback is what makes client-side routing work: the app owns its own
// paths, and a deep link must not 404 just because there is no file at that
// name. Anything under /api, /webhooks, /shroud or /internal is routed
// before this handler is reached, so the fallback cannot swallow them.
func Handler() http.Handler {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		return notBuilt()
	}
	if !Available() {
		return notBuilt()
	}
	// Compressed and ETagged once, up front — see serve.go for the 352KB
	// of uncompressed JavaScript that made this worth doing.
	assets := loadedAssets(sub)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name == "" || name == "." {
			name = "index.html"
		}
		a, ok := assets[name]
		if !ok {
			// Not a file we have. Hand the app its own route.
			a = assets["index.html"]
		}
		if a == nil {
			http.NotFound(w, r)
			return
		}
		a.serve(w, r)
	})
}

// notBuilt says what happened instead of serving a blank page.
//
// A 404 here reads as a broken install; the truth is that this binary was
// compiled without the UI, which is normal in development and a packaging
// mistake in a release.
func notBuilt() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`<!doctype html><meta charset="utf-8">
<title>nanobots — UI not built into this binary</title>
<body style="font:14px/1.6 ui-sans-serif,system-ui;max-width:42rem;margin:4rem auto;padding:0 1rem">
<h1 style="font-size:1.2rem">The UI was not built into this binary</h1>
<p>The API is running and working — this is only the web interface.</p>
<p>For development, run the Vite dev server, which hot-reloads:</p>
<pre style="background:#f4f4f5;padding:.75rem;border-radius:.4rem">cd web &amp;&amp; npm install &amp;&amp; npm run dev</pre>
<p>To build it into the binary instead:</p>
<pre style="background:#f4f4f5;padding:.75rem;border-radius:.4rem">make ui &amp;&amp; go build -o bin/nanobots ./cmd/nanobots</pre>
</body>`))
	})
}
