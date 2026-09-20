// Package catalog carries bots/, examples/swarms/ and roles/roles.yaml
// inside the binary, so `nanobots up` works from anywhere — not only from
// inside a git checkout of this repo.
//
// A released binary (Homebrew, a GitHub release, an npx shim — none of
// which can assume a git clone sits beside them) had a WebUI baked in
// (internal/webui) but nothing to run: BotsDir defaulted to
// filepath.Join(cwd, "bots"), which is empty air anywhere but this repo's
// own root. This package is the other half of "single binary" the WebUI
// embed started.
package catalog

import (
	"embed"
	"fmt"
	"io/fs"
)

// data is filled by `make catalog` (or GoReleaser's before hook), which
// copies bots/, examples/swarms/ and roles/roles.yaml here.
//
// Only .gitkeep is committed — see internal/webui/embed.go's identical
// comment on why an empty embed pattern won't compile on a fresh clone.
//
//go:embed all:data
var data embed.FS

// Available reports whether a real catalog was built into this binary.
// False for a plain `go build` with no `make catalog` first — the normal
// developer loop, which reads bots/ and examples/swarms/ from the checkout
// directly and has no need of this at all.
func Available() bool {
	_, err := data.Open("data/bots/inbox-triage/nanobot.yaml")
	return err == nil
}

// FS returns the embedded catalog, rooted so "bots", "examples/swarms" and
// "roles/roles.yaml" sit exactly where a repo checkout has them — so a
// caller can treat the result the same way it treats a checkout's root.
func FS() (fs.FS, error) {
	if !Available() {
		return nil, fmt.Errorf("no catalog built into this binary — build it with `make catalog` first")
	}
	return fs.Sub(data, "data")
}
