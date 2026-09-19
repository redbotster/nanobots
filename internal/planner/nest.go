package planner

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/redbotster/nanobots/internal/schema"
)

// nestSeparator joins an outer bot id with an inner one, "outer/inner".
// Not "." — that already separates a bot id from its port (ParseEndpoint),
// so an id built with it would misparse as one. Resolve refuses "/" and "."
// in an authored bot id for exactly this reason: the separator this pass
// generates must never collide with one a person wrote.
const nestSeparator = "/"

// maxNestDepth backstops cycle detection for the one case path comparison
// can't see: a swarm nesting back to an in-memory draft that was never
// loaded from a file (PlanSwarm has no path to seed the chain with — see
// its doc comment). A real, useful nesting is one or two levels deep;
// twenty is already deeper than anyone would build by hand.
const maxNestDepth = 20

// InlineNestedSwarms replaces every `swarm:` bot reference with the bots
// and snaps of the swarm it names, entirely in memory, before Resolve ever
// runs. This is the whole feature: the planner, the DAG, the runner,
// retry, fallback, loop and approvals never learn nesting happened,
// because after this pass there is no nesting left to know about — a
// nested swarm's bots are just more bots, and the boundary ports become
// ordinary rewritten snaps. `chain` is the sequence of swarm files already
// being inlined on this path, so a swarm that nests itself (directly, or
// through others) is refused rather than looping forever.
func InlineNestedSwarms(sw *schema.Nanoswarm, botsDir string, chain []string) (*schema.Nanoswarm, error) {
	// Checked here, on every swarm's own authored ids, before anything is
	// renamed: "." already separates a bot id from its port (ParseEndpoint),
	// and "/" is what this pass uses to join an outer id with an inner one
	// once inlining happens. Checking in Resolve instead would also catch
	// the "outer/inner" ids this pass itself generates, which are supposed
	// to contain "/".
	for _, ref := range sw.Spec.Bots {
		if strings.ContainsAny(ref.ID, "./") {
			return nil, fmt.Errorf(`bot %q: a bot id can't contain "." or "/" — both are reserved, by the snap syntax and by nested swarms`, ref.ID)
		}
	}

	hasNested := false
	for _, ref := range sw.Spec.Bots {
		if ref.Swarm != "" {
			hasNested = true
			break
		}
	}
	if !hasNested {
		return sw, nil
	}
	if len(chain) > maxNestDepth {
		return nil, fmt.Errorf("swarm nesting is more than %d levels deep — this is almost certainly a cycle path comparison didn't catch: %s",
			maxNestDepth, strings.Join(chain, " -> "))
	}

	out := *sw
	out.Spec.Bots = nil
	out.Spec.Snaps = append([]schema.Snap{}, sw.Spec.Snaps...)

	// name -> the boundary Ports of the swarm nested at that bot id, needed
	// below to rewrite any outer snap that reads or feeds it.
	boundaries := map[string]schema.Ports{}

	for _, ref := range sw.Spec.Bots {
		if ref.Swarm == "" {
			out.Spec.Bots = append(out.Spec.Bots, ref)
			continue
		}
		if err := onlyUseAndSwarmTogether(ref); err != nil {
			return nil, err
		}
		innerBots, innerSnaps, boundary, err := inlineOneSwarm(ref, sw.SourcePath, botsDir, chain)
		if err != nil {
			return nil, err
		}
		boundaries[ref.ID] = boundary
		out.Spec.Bots = append(out.Spec.Bots, innerBots...)
		out.Spec.Snaps = append(out.Spec.Snaps, innerSnaps...)
	}

	rewritten := make([]schema.Snap, 0, len(out.Spec.Snaps))
	for _, snap := range out.Spec.Snaps {
		from, err := rewriteBoundaryEndpoint(snap.From, boundaries, true)
		if err != nil {
			return nil, fmt.Errorf("snap %s -> %s: %w", snap.From, snap.To, err)
		}
		to, err := rewriteBoundaryEndpoint(snap.To, boundaries, false)
		if err != nil {
			return nil, fmt.Errorf("snap %s -> %s: %w", snap.From, snap.To, err)
		}
		rewritten = append(rewritten, schema.Snap{From: from, To: to, Join: snap.Join})
	}
	out.Spec.Snaps = rewritten

	return &out, nil
}

// onlyUseAndSwarmTogether refuses combining `swarm:` with any of the
// per-instance controls that assume there is one bot to apply them to.
// Once inlined, a `swarm:` node is N bots, not one — "retry the nested
// swarm" or "loop it" is a real idea, but a different, larger one than
// this pass builds, and pretending it works today would mean silently
// doing nothing or, worse, retrying only one of the N.
func onlyUseAndSwarmTogether(ref schema.BotRef) error {
	switch {
	case ref.Retry != 0:
		return fmt.Errorf("bot %q nests a swarm and also sets retry: — a nested swarm's own bots have their own retry, and there is no single bot here for this to apply to", ref.ID)
	case ref.OnError != "" && ref.OnError != schema.OnErrorStop:
		return fmt.Errorf("bot %q nests a swarm and also sets on_error: — set it on the bots inside the nested swarm instead", ref.ID)
	case ref.When != "":
		return fmt.Errorf("bot %q nests a swarm and also sets when: — gate the nested swarm's own entry bot instead", ref.ID)
	case ref.Loop != nil:
		return fmt.Errorf("bot %q nests a swarm and also sets loop: — there is no single bot here for a loop to re-run", ref.ID)
	case ref.Fallback != "":
		return fmt.Errorf("bot %q nests a swarm and also sets fallback: — a nested swarm has no single output shape for a fallback bot to match", ref.ID)
	}
	return nil
}

// inlineOneSwarm loads, recursively inlines, and renames the swarm nested
// at ref, and applies any explicit inputs: the outer swarm gave this node
// to the correct renamed inner bot.
func inlineOneSwarm(ref schema.BotRef, parentDir, botsDir string, chain []string) (bots []schema.BotRef, snaps []schema.Snap, boundary schema.Ports, err error) {
	path := filepath.Join(parentDir, ref.Swarm)
	abs, absErr := filepath.Abs(path)
	if absErr != nil {
		abs = path
	}
	for _, seen := range chain {
		if seen == abs {
			return nil, nil, schema.Ports{}, fmt.Errorf(
				"bot %q: swarm %q nests itself through %s", ref.ID, ref.Swarm, strings.Join(append(chain, abs), " -> "))
		}
	}

	nested, err := schema.LoadNanoswarm(path)
	if err != nil {
		return nil, nil, schema.Ports{}, fmt.Errorf("bot %q: swarm: %q: %w", ref.ID, ref.Swarm, err)
	}
	if len(nested.Spec.Ports.Inputs) == 0 && len(nested.Spec.Ports.Outputs) == 0 {
		return nil, nil, schema.Ports{}, fmt.Errorf(
			"bot %q: swarm: %q declares no ports — a nested swarm needs spec.ports so this node has a typed boundary to snap into",
			ref.ID, ref.Swarm)
	}

	inlined, err := InlineNestedSwarms(nested, botsDir, append(chain, abs))
	if err != nil {
		return nil, nil, schema.Ports{}, err
	}

	prefix := ref.ID + nestSeparator
	renamed := make(map[string]string, len(inlined.Spec.Bots))
	for _, b := range inlined.Spec.Bots {
		renamed[b.ID] = prefix + b.ID
	}

	byNewID := make(map[string]int, len(inlined.Spec.Bots))
	for i, b := range inlined.Spec.Bots {
		b.ID = renamed[b.ID]
		byNewID[b.ID] = i
		bots = append(bots, b)
	}

	for _, s := range inlined.Spec.Snaps {
		from, ferr := renameEndpoint(s.From, renamed)
		if ferr != nil {
			return nil, nil, schema.Ports{}, fmt.Errorf("bot %q: swarm %q: %w", ref.ID, ref.Swarm, ferr)
		}
		to, terr := renameEndpoint(s.To, renamed)
		if terr != nil {
			return nil, nil, schema.Ports{}, fmt.Errorf("bot %q: swarm %q: %w", ref.ID, ref.Swarm, terr)
		}
		snaps = append(snaps, schema.Snap{From: from, To: to, Join: s.Join})
	}

	inputNames := map[string]bool{}
	for _, p := range inlined.Spec.Ports.Inputs {
		inputNames[p.Name] = true
	}
	for name, val := range ref.Inputs {
		if !inputNames[name] {
			return nil, nil, schema.Ports{}, fmt.Errorf(
				"bot %q sets input %q, but swarm %q doesn't declare a port named %q",
				ref.ID, name, ref.Swarm, name)
		}
		mapsTo := mapsToFor(inlined.Spec.Ports.Inputs, name)
		botID, port, merr := splitMapsTo(mapsTo)
		if merr != nil {
			return nil, nil, schema.Ports{}, fmt.Errorf("bot %q: swarm %q: input %q: %w", ref.ID, ref.Swarm, name, merr)
		}
		idx, ok := byNewID[prefix+botID]
		if !ok {
			return nil, nil, schema.Ports{}, fmt.Errorf(
				"bot %q: swarm %q: input %q has maps_to: %q, but %q isn't a bot in that swarm",
				ref.ID, ref.Swarm, name, mapsTo, botID)
		}
		if bots[idx].Inputs == nil {
			bots[idx].Inputs = map[string]any{}
		}
		bots[idx].Inputs[port] = val
	}

	return bots, snaps, inlined.Spec.Ports, nil
}

func mapsToFor(ports []schema.InputPort, name string) string {
	for _, p := range ports {
		if p.Name == name {
			return p.MapsTo
		}
	}
	return ""
}

// splitMapsTo parses a maps_to value as "<bot-id>.<port>" — exactly one
// dot, both sides non-empty. Anything else can't become a valid snap
// endpoint once rewritten, and would otherwise fail later as a confusing
// "bot not found" deep inside the flattened swarm instead of naming the
// actual mistake.
func splitMapsTo(s string) (botID, port string, err error) {
	parts := strings.SplitN(s, ".", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("maps_to: %q — must be exactly \"<bot-id>.<port>\"", s)
	}
	return parts[0], parts[1], nil
}

// renameEndpoint rewrites the bot-id half of a "<bot-id>.<port>[.<field>]"
// endpoint through the rename table, leaving the port and any field path
// untouched.
func renameEndpoint(s string, renamed map[string]string) (string, error) {
	ep, err := ParseEndpoint(s)
	if err != nil {
		return "", err
	}
	newID, ok := renamed[ep.BotID]
	if !ok {
		return "", fmt.Errorf("%q: bot %q not found in this swarm", s, ep.BotID)
	}
	rest := append([]string{ep.Port}, ep.Fields...)
	return newID + "." + strings.Join(rest, "."), nil
}

// rewriteBoundaryEndpoint rewrites an outer snap endpoint that names a
// `swarm:` node's own declared port into the renamed inner bot+port it
// maps to. An endpoint naming an ordinary bot passes through unchanged.
// isFrom picks which half of the boundary's Ports to look the name up in:
// a snap reading a swarm node's output resolves against Outputs, a snap
// feeding one resolves against Inputs. The fan-out marker is refused here
// rather than silently doing something partial — see the doc comment on
// InlineNestedSwarms about what's deliberately not built.
func rewriteBoundaryEndpoint(s string, boundaries map[string]schema.Ports, isFrom bool) (string, error) {
	ep, err := ParseEndpoint(s)
	if err != nil {
		return "", err
	}
	boundary, nested := boundaries[ep.BotID]
	if !nested {
		return s, nil
	}
	if HasFanOutMarker(s) {
		return "", fmt.Errorf("%q: fan-out into or out of a nested swarm isn't built — snap a single value, or fan out a bot inside the nested swarm instead", s)
	}
	var mapsTo string
	if isFrom {
		for _, p := range boundary.Outputs {
			if p.Name == ep.Port {
				mapsTo = p.MapsTo
			}
		}
	} else {
		for _, p := range boundary.Inputs {
			if p.Name == ep.Port {
				mapsTo = p.MapsTo
			}
		}
	}
	if mapsTo == "" {
		return "", fmt.Errorf("%q: %q declares no port named %q", s, ep.BotID, ep.Port)
	}
	botID, port, err := splitMapsTo(mapsTo)
	if err != nil {
		return "", fmt.Errorf("%q: %w", s, err)
	}
	rest := append([]string{port}, ep.Fields...)
	return ep.BotID + nestSeparator + botID + "." + strings.Join(rest, "."), nil
}
