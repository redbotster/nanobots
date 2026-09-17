package runner

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/redbotster/nanobots/internal/planner"
	"github.com/redbotster/nanobots/internal/schema"
	"github.com/redbotster/nanobots/internal/step"
)

// resolveInputs assembles one bot instance's resolved inputs.json: an
// explicit swarm-level value wins, then an upstream snap, then the port's
// own default, in that order — matching how the planner already validated
// these same three sources type-check.
// fanIndex is the element a fanned-out iteration is working on, or -1 when
// the bot runs once. Threaded through input resolution so a "*" in a snap
// path resolves to that one element.
type fanIndex int

const noFan fanIndex = -1

func (o *Orchestrator) resolveInputs(run *Run, rs *planner.ResolvedSwarm, botID string, rb *planner.ResolvedBot) (map[string]any, error) {
	return o.resolveInputsAt(run, rs, botID, rb, noFan)
}

func (o *Orchestrator) resolveInputsAt(run *Run, rs *planner.ResolvedSwarm, botID string, rb *planner.ResolvedBot, at fanIndex) (map[string]any, error) {
	snapsTo := map[string]schema.Snap{}
	for _, snap := range rs.Swarm.Spec.Snaps {
		to, err := planner.ParseEndpoint(snap.To)
		if err != nil {
			return nil, err
		}
		if to.BotID == botID {
			snapsTo[to.Port] = snap
		}
	}

	ctx := map[string]any{
		"vars": rs.Swarm.Spec.Vars,
		"run":  map[string]any{"date": time.Now().Format("2006-01-02")},
		// Resolved per run rather than baked into a bot's YAML, so editing
		// a role in the UI changes the next review immediately and no bot
		// file has to be rewritten. Empty when no library is configured,
		// which is how review-board falls back to inventing a team.
		"roles": map[string]any{"roster": o.roster(run)},
		// Whatever started this run carried. Always present and usually
		// empty, so `{{trigger.payload | default: vars.example}}` works
		// for both a webhook and someone pressing Run.
		"trigger": map[string]any{"payload": run.TriggerPayload},
		"memory":  map[string]any{},
	}

	inputs := map[string]any{}
	for _, port := range rb.Nanobot.Spec.Ports.Inputs {
		if explicit, ok := rb.Ref.Inputs[port.Name]; ok {
			inputs[port.Name] = step.ResolveTemplateValue(explicit, ctx)
			continue
		}
		if snap, ok := snapsTo[port.Name]; ok {
			val, err := o.resolveSnapValueAt(run, snap, at)
			if err != nil {
				return nil, fmt.Errorf("input %q: %w", port.Name, err)
			}
			inputs[port.Name] = val
			continue
		}
		if port.Default != "" {
			inputs[port.Name] = step.ResolveTemplateValue(port.Default, ctx)
			continue
		}
		if port.Required {
			return nil, fmt.Errorf("input %q has no explicit value, snap, or default", port.Name)
		}
	}
	return inputs, nil
}

// resolveSnapValue reads the already-computed upstream bot's output and
// drills into any extra dotted-path fields the snap names (the planner
// already proved this is a json port with a matching schema field — see
// planner.TypeCheckSnaps — so this only has to do the lookup, not validate
// it's legal).
func (o *Orchestrator) resolveSnapValue(run *Run, snap schema.Snap) (any, error) {
	return o.resolveSnapValueAt(run, snap, noFan)
}

func (o *Orchestrator) resolveSnapValueAt(run *Run, snap schema.Snap, at fanIndex) (any, error) {
	from, err := planner.ParseEndpoint(snap.From)
	if err != nil {
		return nil, err
	}
	upstream, ok := run.BotOutputs(from.BotID)
	if !ok {
		return nil, fmt.Errorf("upstream bot %q has no recorded outputs yet (snap %s -> %s)", from.BotID, snap.From, snap.To)
	}
	val, ok := upstream[from.Port]
	if !ok {
		return nil, fmt.Errorf("upstream bot %q produced no output %q", from.BotID, from.Port)
	}
	for _, field := range from.Fields {
		if field == planner.FanOutMarker {
			arr, ok := val.([]any)
			if !ok {
				return nil, fmt.Errorf("%s is not a list, so there is nothing for %s to iterate over",
					snap.From, planner.FanOutMarker)
			}
			if at == noFan {
				return nil, fmt.Errorf("internal: %s resolved without a fan-out index", snap.From)
			}
			if int(at) >= len(arr) {
				return nil, fmt.Errorf("%s has %d item(s), no element %d", snap.From, len(arr), at)
			}
			val = arr[at]
			continue
		}
		if idx, isIndex := step.ListIndex(field); isIndex {
			arr, ok := val.([]any)
			if !ok {
				return nil, fmt.Errorf("%s.%s is not a list, cannot index [%d]", from.BotID, from.Port, idx)
			}
			if idx < 0 || idx >= len(arr) {
				return nil, fmt.Errorf("%s.%s has %d item(s), index %d is out of range", from.BotID, from.Port, len(arr), idx)
			}
			val = arr[idx]
			continue
		}
		m, ok := val.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s.%s is not an object, cannot read field %q", from.BotID, from.Port, field)
		}
		val, ok = m[field]
		if !ok {
			return nil, fmt.Errorf("%s.%s has no field %q", from.BotID, from.Port, field)
		}
	}
	// Last, after any field access: `join` collapses what the fan-out
	// produced, and the planner type-checked it against exactly this
	// value's type. See internal/planner/join.go.
	if snap.Join != "" {
		joined, err := planner.JoinValue(planner.JoinMode(snap.Join), val)
		if err != nil {
			return nil, fmt.Errorf("snap %s -> %s: %w", snap.From, snap.To, err)
		}
		return joined, nil
	}
	return val, nil
}

// collectOutputs reads a finished container's /run/outputs (bind-mounted at
// runDir/outputs on the host) and produces the bot's declared output ports:
// <port>.json for scalar/json/list values, or raw bytes at <port> (+
// <port>.mime) for a file port, which get moved into nanobotd's own
// persistent blob store so later bots (running in their own, separate
// containers) and the WebUI can both reach them by nbf:// reference.
// NothingToDoMarker is how a container says it stopped early on purpose.
//
// The in-process path returns a value; a container only has its filesystem
// and an exit code, and exit 0 with no outputs is indistinguishable from a
// bot that forgot to write any. So the agent leaves this file, holding the
// reason, and collectOutputs reads it before deciding anything is missing.
// Named in docs/bot-contract.md, because any container honouring the
// contract may write it.
const NothingToDoMarker = ".nothing-to-do"

func collectOutputs(nb *schema.Nanobot, blobs step.BlobStore, outDir string) (map[string]any, error) {
	if raw, err := os.ReadFile(filepath.Join(outDir, NothingToDoMarker)); err == nil {
		reason := strings.TrimSpace(string(raw))
		if reason == "" {
			reason = "nothing to do"
		}
		return nil, &NothingToDoError{Bot: nb.Metadata.Name, Reason: reason}
	}
	out := map[string]any{}
	for _, port := range nb.Spec.Ports.Outputs {
		jsonPath := filepath.Join(outDir, port.Name+".json")
		if raw, err := os.ReadFile(jsonPath); err == nil {
			var v any
			if err := json.Unmarshal(raw, &v); err != nil {
				return nil, fmt.Errorf("output %q: parse %s: %w", port.Name, jsonPath, err)
			}
			out[port.Name] = v
			continue
		}

		filePath := filepath.Join(outDir, port.Name)
		data, err := os.ReadFile(filePath)
		if err != nil {
			return nil, fmt.Errorf("declared output port %q was never produced", port.Name)
		}
		mime, _ := os.ReadFile(filePath + ".mime")
		fv, err := blobs.Write(data, string(mime))
		if err != nil {
			return nil, fmt.Errorf("output %q: store blob: %w", port.Name, err)
		}
		out[port.Name] = map[string]any{"uri": fv.URI, "mime": fv.Mime}
	}
	return out, nil
}

// fanOutLength counts the items a marker-carrying snap will iterate over,
// by resolving its path up to the marker and measuring the list there.
func (o *Orchestrator) fanOutLength(run *Run, snap schema.Snap) (int, error) {
	from, err := planner.ParseEndpoint(snap.From)
	if err != nil {
		return 0, err
	}
	upstream, ok := run.BotOutputs(from.BotID)
	if !ok {
		return 0, fmt.Errorf("upstream bot %q has no recorded outputs yet (snap %s -> %s)", from.BotID, snap.From, snap.To)
	}
	val, ok := upstream[from.Port]
	if !ok {
		return 0, fmt.Errorf("upstream bot %q produced no output %q", from.BotID, from.Port)
	}
	for _, field := range from.Fields {
		if field == planner.FanOutMarker {
			arr, ok := val.([]any)
			if !ok {
				return 0, fmt.Errorf("%s is not a list, so there is nothing for %s to iterate over",
					snap.From, planner.FanOutMarker)
			}
			return len(arr), nil
		}
		if idx, isIndex := step.ListIndex(field); isIndex {
			arr, ok := val.([]any)
			if !ok || idx < 0 || idx >= len(arr) {
				return 0, fmt.Errorf("%s: cannot index [%d]", snap.From, idx)
			}
			val = arr[idx]
			continue
		}
		m, ok := val.(map[string]any)
		if !ok {
			return 0, fmt.Errorf("%s is not an object, cannot read field %q", snap.From, field)
		}
		if val, ok = m[field]; !ok {
			return 0, fmt.Errorf("%s has no field %q", snap.From, field)
		}
	}
	return 0, fmt.Errorf("%s carries no %s marker", snap.From, planner.FanOutMarker)
}

// roster renders the live role library for {{roles.roster}}.
//
// A failure here is logged and treated as no roster rather than failing
// the run: a review board with no roster invents a team, which is exactly
// what it did before the library existed, and is a far better outcome than
// refusing to review anything because a JSON file is malformed.
func (o *Orchestrator) roster(run *Run) string {
	if o.Roles == nil {
		return ""
	}
	text, err := o.Roles.Roster()
	if err != nil {
		run.Log("", "", "role library: %v — reviewers will be chosen without it", err)
	}
	return text
}
