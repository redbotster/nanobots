package runner

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/redbotster/nanobots/internal/planner"
	"github.com/redbotster/nanobots/internal/schema"
	"github.com/redbotster/nanobots/internal/step"
)

// resolveInputs assembles one bot instance's resolved inputs.json: an
// explicit swarm-level value wins, then an upstream snap, then the port's
// own default, in that order — matching how the planner already validated
// these same three sources type-check.
func (o *Orchestrator) resolveInputs(run *Run, rs *planner.ResolvedSwarm, botID string, rb *planner.ResolvedBot) (map[string]any, error) {
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
		"vars":   rs.Swarm.Spec.Vars,
		"run":    map[string]any{"date": time.Now().Format("2006-01-02")},
		"memory": map[string]any{},
	}

	inputs := map[string]any{}
	for _, port := range rb.Nanobot.Spec.Ports.Inputs {
		if explicit, ok := rb.Ref.Inputs[port.Name]; ok {
			inputs[port.Name] = step.ResolveTemplateValue(explicit, ctx)
			continue
		}
		if snap, ok := snapsTo[port.Name]; ok {
			val, err := o.resolveSnapValue(run, snap)
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
		m, ok := val.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s.%s is not an object, cannot read field %q", from.BotID, from.Port, field)
		}
		val, ok = m[field]
		if !ok {
			return nil, fmt.Errorf("%s.%s has no field %q", from.BotID, from.Port, field)
		}
	}
	return val, nil
}

// collectOutputs reads a finished container's /run/outputs (bind-mounted at
// runDir/outputs on the host) and produces the bot's declared output ports:
// <port>.json for scalar/json/list values, or raw bytes at <port> (+
// <port>.mime) for a file port, which get moved into nanobotd's own
// persistent blob store so later bots (running in their own, separate
// containers) and the WebUI can both reach them by nbf:// reference.
func collectOutputs(nb *schema.Nanobot, blobs step.BlobStore, outDir string) (map[string]any, error) {
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
