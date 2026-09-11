package step

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/redbotster/nanobots/internal/schema"
)

// LogLine is one line of the bot's run log, in the shape the runner streams
// over SSE.
type LogLine struct {
	Step string
	Msg  string
}

// Result is what Interpret produces: the bot's declared output ports, filled
// in and loosely type-checked against schema.OutputPort.Type.
type Result struct {
	Outputs map[string]any
	Log     []LogLine
}

// Interpret runs every step in nb.Spec.Steps, in order, against resolvedInputs
// (already defaulted/validated by the caller) and deps. swarmVars is exposed
// to step templates as `{{swarm.vars.*}}` (see e.g.
// bots/recap-emails-to-pdf/nanobot.yaml's upload step) — pass nil for a bot
// run outside any swarm context. It returns an error on the first step that
// fails, or if a declared output port is never produced.
func Interpret(nb *schema.Nanobot, resolvedInputs map[string]any, swarmVars map[string]any, deps Deps) (*Result, error) {
	ctx := map[string]any{
		"inputs":  resolvedInputs,
		"steps":   map[string]any{},
		"outputs": map[string]any{},
		"memory":  map[string]any{}, // populated lazily by memory.get below
		"run":     map[string]any{"started_at": deps.Now()},
		"swarm":   map[string]any{"vars": swarmVars},
	}
	stepsCtx := ctx["steps"].(map[string]any)
	outputsCtx := ctx["outputs"].(map[string]any)

	res := &Result{Outputs: map[string]any{}}
	log := func(stepName, format string, a ...any) {
		res.Log = append(res.Log, LogLine{Step: stepName, Msg: fmt.Sprintf(format, a...)})
	}

	var lastOutput any
	for _, s := range nb.Spec.Steps {
		var out any
		var err error

		switch s.Type {
		case "service.call":
			svc, ok := findService(nb, s.Service)
			if !ok {
				return nil, fmt.Errorf("step %q: no service %q declared on this bot", s.Name, s.Service)
			}
			params, _ := resolveValue(s.Params, ctx).(map[string]any)
			out, err = deps.ServiceCall(svc, s.Op, params)
			if err == nil {
				log(s.Name, "%s.%s -> ok", s.Service, s.Op)
				out, err = maybeMaterializeFile(nb, s, out, deps)
			}

		case "ai.generate":
			out, err = runAIGenerate(nb, s, ctx, deps)
			if err == nil {
				log(s.Name, "ai.generate via prompt_file %s -> ok", s.PromptFile)
			}

		case "transform.render":
			out, err = runRender(nb, s, ctx, lastOutput, deps)
			if err == nil {
				fv, _ := out.(FileValue)
				log(s.Name, "rendered %s -> %s", s.Template, fv.URI)
			}

		case "transform.now":
			out = deps.Now()

		case "web.fetch":
			// No service, no credential — runs identically under Demo/Live
			// (see runWebFetch/fetchURL in webfetch.go).
			params, _ := resolveValue(s.Params, ctx).(map[string]any)
			out, err = runWebFetch(params)
			if err == nil {
				log(s.Name, "web.fetch -> ok")
			}

		case "transform.pick":
			// A pure computation step: no service, no model, just resolve
			// s.Value (which may be a whole literal/object, not just a
			// string) against ctx. Exists so a step can shape a value —
			// picking a field out of an earlier step, building a small
			// literal like an event payload — without pretending it's a
			// service call. See schema.Step.Outputs for pulling several
			// fields out of one step at once.
			out = resolveValue(s.Data, ctx)

		case "memory.get":
			// Best-effort: memory is "since last run" bookkeeping, not a
			// correctness requirement — a backend that can't store it for
			// any reason degrades to "nothing remembered" rather than
			// failing the whole run. (Was load-bearing for a real gap —
			// memory_enabled had no way to turn on via the public API —
			// fixed in @1claw/openapi-spec 0.61.1; see docs/oneclaw-bridge.md.)
			var found bool
			var memErr error
			out, found, memErr = deps.MemoryGet(nb.Metadata.Name, s.Key)
			if memErr != nil {
				log(s.Name, "memory.get %s failed, continuing without it: %v", s.Key, memErr)
				out, found = "", false
			} else {
				log(s.Name, "memory.get %s (found=%v)", s.Key, found)
			}

		case "memory.put":
			val := fmt.Sprint(resolveValue(s.Value, ctx))
			out = val
			if memErr := deps.MemoryPut(nb.Metadata.Name, s.Key, val); memErr != nil {
				log(s.Name, "memory.put %s failed, continuing without it: %v", s.Key, memErr)
			} else {
				log(s.Name, "memory.put %s", s.Key)
			}

		case "approve":
			summary := fmt.Sprint(resolveValue(s.Summary, ctx))
			var approved bool
			var decidedBy string
			approved, decidedBy, err = deps.Approve(summary, s.RiskTier)
			out = map[string]any{"approved": approved, "decided_by": decidedBy}
			if err == nil {
				log(s.Name, "approve %q -> approved=%v by=%s", summary, approved, decidedBy)
				// A rejection only aborts the run when nothing downstream is
				// set up to look at the decision — that's an inline gate
				// (e.g. email-drive-file's "gate" step before sending).
				// When the step binds an output (the catalog's standalone
				// `approve` brick), false is a normal, valid result: the
				// swarm around it decides what to do with a "no", not this
				// bot.
				if !approved && s.Output == "" && len(s.Outputs) == 0 {
					return nil, fmt.Errorf("step %q: not approved (decided_by=%s)", s.Name, decidedBy)
				}
			}

		case "notify":
			message, _ := resolveValue(s.Params["message"], ctx).(string)
			channel, _ := resolveValue(s.Params["channel"], ctx).(string)
			err = deps.Notify(message, channel)
			// A bare bool, not {"delivered": bool} — the catalog's `notify`
			// brick declares a single `delivered:boolean` output port, so
			// that's the shape a step.Output binding needs to match.
			out = err == nil
			if err == nil {
				log(s.Name, "notify -> %s", channel)
			}

		default:
			err = fmt.Errorf("step type %q is not implemented", s.Type)
		}

		if err != nil {
			return nil, fmt.Errorf("step %q: %w", s.Name, err)
		}

		stepsCtx[s.Name] = map[string]any{"output": out}
		if s.Output != "" {
			outputsCtx[s.Output] = out
		}
		for port, tmpl := range s.Outputs {
			outputsCtx[port] = resolveValue(tmpl, ctx)
		}
		lastOutput = out
	}

	for _, port := range nb.Spec.Ports.Outputs {
		val, ok := outputsCtx[port.Name]
		if !ok {
			return nil, fmt.Errorf("declared output port %q was never produced by any step", port.Name)
		}
		if err := validateOutputType(val, port.Type); err != nil {
			return nil, fmt.Errorf("output port %q: %w", port.Name, err)
		}
		res.Outputs[port.Name] = val
	}
	return res, nil
}

// maybeMaterializeFile turns a service.call result into a FileValue when the
// step's declared output port is `file`-typed and the op returned the
// blueprint's file-download shape ({"content_base64": "...", "mime": "..."}).
// This is how a bot like drive-watch turns "downloaded a file from Drive"
// into a real blob-store reference without every provider needing its own
// bespoke handling — any op that returns that shape gets this for free.
func maybeMaterializeFile(nb *schema.Nanobot, s schema.Step, out any, deps Deps) (any, error) {
	if s.Output == "" {
		return out, nil
	}
	var portType string
	for _, p := range nb.Spec.Ports.Outputs {
		if p.Name == s.Output {
			portType = p.Type
			break
		}
	}
	if portType != string(schema.PortFile) {
		return out, nil
	}
	m, ok := out.(map[string]any)
	if !ok {
		return out, nil
	}
	b64, ok := m["content_base64"].(string)
	if !ok {
		return out, nil
	}
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, fmt.Errorf("decode content_base64: %w", err)
	}
	mime, _ := m["mime"].(string)
	return deps.Blobs().Write(data, mime)
}

func findService(nb *schema.Nanobot, id string) (schema.Service, bool) {
	for _, s := range nb.Spec.Services {
		if s.ID == id {
			return s, true
		}
	}
	return schema.Service{}, false
}

// runAIGenerate resolves the step's `inputs:` map against ctx, renders
// prompt_file with those as top-level template variables, calls the model,
// and — since every ai.generate step in this build's two example bots
// declares a json output — parses the response as JSON.
func runAIGenerate(nb *schema.Nanobot, s schema.Step, ctx map[string]any, deps Deps) (any, error) {
	promptPath := filepath.Join(nb.SourcePath, s.PromptFile)
	tmpl, err := readFile(promptPath)
	if err != nil {
		return nil, fmt.Errorf("read prompt_file: %w", err)
	}
	vars := map[string]any{}
	for k, v := range s.Inputs {
		vars[k] = resolveValue(v, ctx)
	}
	prompt := renderPromptVars(tmpl, vars)

	raw, err := deps.AIGenerate(prompt, nb.Spec.Model)
	if err != nil {
		return nil, err
	}
	cleaned := stripCodeFence(raw)
	var parsed any
	if err := json.Unmarshal([]byte(cleaned), &parsed); err != nil {
		return nil, fmt.Errorf("model response was not valid JSON: %w (response: %s)", err, truncate(raw, 200))
	}
	return parsed, nil
}

func runRender(nb *schema.Nanobot, s schema.Step, ctx map[string]any, lastOutput any, deps Deps) (any, error) {
	data := lastOutput
	if d, ok := s.Inputs["data"]; ok {
		data = resolveValue(d, ctx)
	}
	templatePath := filepath.Join(nb.SourcePath, s.Template)
	bytes, mime, err := deps.Render(templatePath, data, s.To)
	if err != nil {
		return nil, err
	}
	return deps.Blobs().Write(bytes, mime)
}

func renderPromptVars(tmpl string, vars map[string]any) string {
	return templateExpr.ReplaceAllStringFunc(tmpl, func(m string) string {
		key := strings.TrimSpace(m[2 : len(m)-2])
		val, ok := vars[key]
		if !ok {
			return m
		}
		if str, ok := val.(string); ok {
			return str
		}
		b, err := json.Marshal(val)
		if err != nil {
			return fmt.Sprint(val)
		}
		return string(b)
	})
}

func stripCodeFence(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	return strings.TrimSpace(s)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func validateOutputType(val any, portType string) error {
	pt, err := schema.ParsePortType(portType)
	if err != nil {
		return err
	}
	return checkType(val, pt)
}

func checkType(val any, pt schema.ParsedType) error {
	if pt.Base == "list" {
		arr, ok := val.([]any)
		if !ok {
			return fmt.Errorf("expected a list, got %T", val)
		}
		for i, el := range arr {
			if err := checkType(el, *pt.List); err != nil {
				return fmt.Errorf("item %d: %w", i, err)
			}
		}
		return nil
	}
	switch pt.Base {
	case schema.PortString, schema.PortDatetime:
		if _, ok := val.(string); !ok {
			return fmt.Errorf("expected a string, got %T", val)
		}
	case schema.PortBoolean:
		if _, ok := val.(bool); !ok {
			return fmt.Errorf("expected a boolean, got %T", val)
		}
	case schema.PortFile:
		if _, ok := val.(FileValue); !ok {
			return fmt.Errorf("expected a file reference, got %T", val)
		}
	case schema.PortJSON:
		switch val.(type) {
		case map[string]any, []any:
			// ok
		default:
			return fmt.Errorf("expected a json object or array, got %T", val)
		}
	case schema.PortEvent:
		// events are opaque for now; anything goes.
	}
	return nil
}
