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
	// Fallbacks holds the resolved fallback: bot for every instance that
	// declares one, keyed by the same bot instance id as Bots. Only
	// entries that declared fallback: appear here.
	Fallbacks map[string]*ResolvedBot
}

// Resolve loads every bot a swarm references. `use: name@version` refs are
// resolved from botsDir/<name>/nanobot.yaml (the version suffix is checked
// against metadata.version but not otherwise fetched — there is no registry
// yet, see blueprint §4 #8). `path:` refs are resolved relative to the
// swarm file's own directory. `fallback: name@version`, when present, is
// resolved the same way `use:` is — a fallback is always a registry ref,
// never a local path.
func Resolve(sw *schema.Nanoswarm, botsDir string) (*ResolvedSwarm, error) {
	out := &ResolvedSwarm{Swarm: sw, Bots: map[string]*ResolvedBot{}, Fallbacks: map[string]*ResolvedBot{}}
	for _, ref := range sw.Spec.Bots {
		if ref.ID == "" {
			return nil, fmt.Errorf("bot entry missing id")
		}
		if _, dup := out.Bots[ref.ID]; dup {
			return nil, fmt.Errorf("duplicate bot id %q", ref.ID)
		}
		var nb *schema.Nanobot
		var err error
		switch {
		case ref.Path != "":
			nb, err = schema.LoadNanobot(filepath.Join(sw.SourcePath, ref.Path, "nanobot.yaml"))
			if err != nil {
				err = fmt.Errorf("bot %q: %w", ref.ID, err)
			}
		case ref.Use != "":
			nb, err = resolveUse(ref.Use, botsDir, ref.ID)
		default:
			err = fmt.Errorf("bot %q: must set either \"use\" or \"path\"", ref.ID)
		}
		if err != nil {
			return nil, err
		}
		out.Bots[ref.ID] = &ResolvedBot{Ref: ref, Nanobot: nb}

		if ref.Fallback != "" {
			fb, err := resolveUse(ref.Fallback, botsDir, ref.ID)
			if err != nil {
				return nil, fmt.Errorf("bot %q: fallback: %w", ref.ID, err)
			}
			out.Fallbacks[ref.ID] = &ResolvedBot{Ref: ref, Nanobot: fb}
		}
	}
	return out, nil
}

// resolveUse loads a "name@version" registry reference from
// botsDir/name/nanobot.yaml, checking the version against what's actually on
// disk — there is no registry yet (blueprint §4 #8), so this is the only
// check available.
func resolveUse(use, botsDir, refID string) (*schema.Nanobot, error) {
	name, version, _ := strings.Cut(use, "@")
	nb, err := schema.LoadNanobot(filepath.Join(botsDir, name, "nanobot.yaml"))
	if err != nil {
		return nil, fmt.Errorf("bot %q: resolving %q: %w", refID, use, err)
	}
	if version != "" && nb.Metadata.Version != version {
		return nil, fmt.Errorf("bot %q: %s@%s requested but %s/nanobot.yaml is version %s",
			refID, name, version, name, nb.Metadata.Version)
	}
	return nb, nil
}
