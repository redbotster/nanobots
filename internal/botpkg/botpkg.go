// Package botpkg is where "use: name@version" actually means something.
//
// Before this package existed, that resolution — walk to
// botsDir/name/nanobot.yaml, and if a version was requested, check it
// against what's on disk — was reimplemented separately everywhere a bot
// was loaded: the planner, the WebUI's bot listing, `nanobots import`,
// foundry's promote step, and the container warm-up path all had their own
// copy. Only the planner's copy actually checked the version. `nanobots
// import`'s did not (cmd/nanobots/share.go's old catalogLookup discarded
// everything after the "@"), so it could report "you have everything this
// bundle needs" for a bundle naming invoice-chaser@2.0.0 while only 0.1.0
// was on disk — the real mismatch surfaced later, confusingly, at plan or
// run time instead of at import, where it was actually knowable.
//
// There is no registry yet. Source is built so that adding one later is
// additive — a RemoteSource implementing the same interface — rather than
// a rewrite: every caller already goes through Resolve/List instead of its
// own filesystem walk. See docs/bot-packages.md.
package botpkg

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/redbotster/nanobots/internal/schema"
)

// Source resolves a bot package by name and, optionally, an exact version.
type Source interface {
	// Resolve loads the bot named name. If version is non-empty it must
	// equal the bot's own metadata.version exactly — string equality, not
	// semver, because a LocalDir has nowhere to put a second version of
	// the same bot; there is nothing yet to range over.
	Resolve(name, version string) (*schema.Nanobot, error)
	// List returns the name of every bot this source can resolve, sorted.
	List() ([]string, error)
}

// LocalDir is the only Source this build has: one flat directory,
// <Dir>/<name>/nanobot.yaml — today's entire catalog layout, unchanged.
type LocalDir struct {
	Dir string
}

func (l LocalDir) Resolve(name, version string) (*schema.Nanobot, error) {
	path := filepath.Join(l.Dir, name, "nanobot.yaml")
	nb, err := schema.LoadNanobot(path)
	if err != nil {
		return nil, err
	}
	if version != "" && nb.Metadata.Version != version {
		return nil, fmt.Errorf("%s@%s requested but %s is version %s", name, version, path, nb.Metadata.Version)
	}
	return nb, nil
}

func (l LocalDir) List() ([]string, error) {
	entries, err := os.ReadDir(l.Dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(l.Dir, e.Name(), "nanobot.yaml")); err != nil {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names, nil
}

// ParseRef splits a "name@version" registry reference. version is "" when
// none was given — every existing caller treats a bare name as "whatever
// version is currently installed."
func ParseRef(use string) (name, version string) {
	name, version, _ = strings.Cut(use, "@")
	return name, version
}
