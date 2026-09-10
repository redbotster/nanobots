// Package planner parses a Nanoswarm, resolves its bot references, type-checks
// its snaps against the bots' declared ports, and builds the run DAG. This is
// "nanobots plan" — see NANOBOTS-BLUEPRINT.md §6 step 3.
package planner

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/redbotster/nanobots/internal/schema"
)

// ResolvedBot pairs a swarm's bot reference with the Nanobot it resolved to.
type ResolvedBot struct {
	Ref     schema.BotRef
	Nanobot *schema.Nanobot
}

// ResolvedSwarm is a Nanoswarm with every bots[] entry resolved to a loaded
// Nanobot definition.
type ResolvedSwarm struct {
	Swarm *schema.Nanoswarm
	Bots  map[string]*ResolvedBot // keyed by bot instance id
}

// Resolve loads every bot a swarm references. `use: name@version` refs are
// resolved from botsDir/<name>/nanobot.yaml (the version suffix is checked
// against metadata.version but not otherwise fetched — there is no registry
// yet, see blueprint §4 #8). `path:` refs are resolved relative to the
// swarm file's own directory.
func Resolve(sw *schema.Nanoswarm, botsDir string) (*ResolvedSwarm, error) {
	out := &ResolvedSwarm{Swarm: sw, Bots: map[string]*ResolvedBot{}}
	for _, ref := range sw.Spec.Bots {
		if ref.ID == "" {
			return nil, fmt.Errorf("bot entry missing id")
		}
		if _, dup := out.Bots[ref.ID]; dup {
			return nil, fmt.Errorf("duplicate bot id %q", ref.ID)
		}
		var nbPath string
		switch {
		case ref.Path != "":
			nbPath = filepath.Join(sw.SourcePath, ref.Path, "nanobot.yaml")
		case ref.Use != "":
			name, version, _ := strings.Cut(ref.Use, "@")
			nbPath = filepath.Join(botsDir, name, "nanobot.yaml")
			nb, err := schema.LoadNanobot(nbPath)
			if err != nil {
				return nil, fmt.Errorf("bot %q: resolving %q: %w", ref.ID, ref.Use, err)
			}
			if version != "" && nb.Metadata.Version != version {
				return nil, fmt.Errorf("bot %q: %s@%s requested but %s/nanobot.yaml is version %s",
					ref.ID, name, version, name, nb.Metadata.Version)
			}
			out.Bots[ref.ID] = &ResolvedBot{Ref: ref, Nanobot: nb}
			continue
		default:
			return nil, fmt.Errorf("bot %q: must set either use: or path:", ref.ID)
		}
		nb, err := schema.LoadNanobot(nbPath)
		if err != nil {
			return nil, fmt.Errorf("bot %q: %w", ref.ID, err)
		}
		out.Bots[ref.ID] = &ResolvedBot{Ref: ref, Nanobot: nb}
	}
	return out, nil
}
