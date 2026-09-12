package planner

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/redbotster/nanobots/internal/schema"
	"github.com/redbotster/nanobots/internal/step"
)

// SnapCheck is the outcome of type-checking one snap.
type SnapCheck struct {
	Snap     schema.Snap
	FromType schema.ParsedType
	ToType   schema.ParsedType
	OK       bool
	Err      error
}

// Endpoint is a parsed "<bot-id>.<port>[.<field>...]" reference, exported so
// internal/runner can resolve a snap's actual value (not just its type) when
// wiring a downstream bot's inputs at run time.
type Endpoint struct {
	BotID  string
	Port   string
	Fields []string // remaining dotted segments, for drilling into a json-typed port's schema
}

// ParseEndpoint parses "<bot-id>.<port>[.<field>...]" — the shape of both
// sides of a Nanoswarm snap.
func ParseEndpoint(s string) (Endpoint, error) {
	parts := strings.Split(s, ".")
	if len(parts) < 2 {
		return Endpoint{}, fmt.Errorf("%q: expected <bot-id>.<port>[.<field>...]", s)
	}
	return Endpoint{BotID: parts[0], Port: parts[1], Fields: parts[2:]}, nil
}

// TypeCheckSnaps validates every snap in the swarm: both endpoints must
// resolve to a real bot and port, and the resolved types must be assignable
// (schema.Assignable). A `from` endpoint may drill into a json-typed output
// port's referenced schema via extra dotted segments (e.g.
// `recap.recap_json.headline`); resolveFieldType walks that schema to find
// the leaf field's type before the assignability check runs, so Assignable
// itself never has to know about json field access.
func TypeCheckSnaps(rs *ResolvedSwarm) []SnapCheck {
	var results []SnapCheck
	for _, snap := range rs.Swarm.Spec.Snaps {
		check := SnapCheck{Snap: snap}
		fromType, err := resolveEndpointType(rs, snap.From, true)
		if err != nil {
			check.Err = fmt.Errorf("from %q: %w", snap.From, err)
			results = append(results, check)
			continue
		}
		toType, err := resolveEndpointType(rs, snap.To, false)
		if err != nil {
			check.Err = fmt.Errorf("to %q: %w", snap.To, err)
			results = append(results, check)
			continue
		}
		check.FromType, check.ToType = fromType, toType
		if !schema.Assignable(fromType, toType) {
			check.Err = fmt.Errorf("cannot snap %s (%s) to %s (%s): types are not assignable",
				snap.From, fromType, snap.To, toType)
		} else {
			check.OK = true
		}
		results = append(results, check)
	}
	return results
}

// resolveEndpointType resolves a snap endpoint's type. For an output
// endpoint on a bot that fans out, the result is wrapped in list<> —
// running a bot twenty times produces twenty of each of its outputs, and
// the swarm's type checking has to see that or a downstream bot would be
// promised a single value it will never get.
func resolveEndpointType(rs *ResolvedSwarm, ref string, isOutput bool) (schema.ParsedType, error) {
	t, err := resolveDeclaredEndpointType(rs, ref, isOutput)
	if err != nil {
		return t, err
	}
	if !isOutput {
		return t, nil
	}
	ep, err := ParseEndpoint(ref)
	if err != nil {
		return t, err
	}
	// Reading one element back out (`sender.message_id.0`) or fanning over
	// it again already consumed the extra level, so only wrap when the
	// reference names the whole port.
	if len(ep.Fields) == 0 && IsFannedOut(rs.Swarm, ep.BotID) {
		return schema.ParsedType{Base: "list", List: &t}, nil
	}
	return t, nil
}

func resolveDeclaredEndpointType(rs *ResolvedSwarm, ref string, isOutput bool) (schema.ParsedType, error) {
	ep, err := ParseEndpoint(ref)
	if err != nil {
		return schema.ParsedType{}, err
	}
	rb, ok := rs.Bots[ep.BotID]
	if !ok {
		return schema.ParsedType{}, fmt.Errorf("no bot with id %q in this swarm", ep.BotID)
	}
	var typeStr, schemaFile string
	if isOutput {
		found := false
		for _, p := range rb.Nanobot.Spec.Ports.Outputs {
			if p.Name == ep.Port {
				typeStr, schemaFile, found = p.Type, p.Schema, true
				break
			}
		}
		if !found {
			return schema.ParsedType{}, fmt.Errorf("bot %q has no output port %q", ep.BotID, ep.Port)
		}
	} else {
		found := false
		for _, p := range rb.Nanobot.Spec.Ports.Inputs {
			if p.Name == ep.Port {
				typeStr, found = p.Type, true
				break
			}
		}
		if !found {
			return schema.ParsedType{}, fmt.Errorf("bot %q has no input port %q", ep.BotID, ep.Port)
		}
		if len(ep.Fields) > 0 {
			return schema.ParsedType{}, fmt.Errorf("input port reference %q cannot drill into fields", ref)
		}
	}
	base, err := schema.ParsePortType(typeStr)
	if err != nil {
		return schema.ParsedType{}, fmt.Errorf("bot %q port %q: %w", ep.BotID, ep.Port, err)
	}
	fields := ep.Fields
	// A purely-numeric segment indexes into a list<T> port, peeling off one
	// level of nesting per index — "draft_ids.0" on a list<string> port
	// resolves to plain string, no schema file needed (there's nothing to
	// look up: the element type is already known from the port declaration
	// itself). See internal/step.ListIndex for the matching runtime rule.
	for base.Base == "list" && len(fields) > 0 {
		// "*" peels a list level exactly as an index does, but means every
		// element rather than one — the bot downstream runs once per item.
		// See planner/fanout.go.
		if fields[0] == FanOutMarker {
			base = *base.List
			fields = fields[1:]
			continue
		}
		if _, isIndex := step.ListIndex(fields[0]); !isIndex {
			return schema.ParsedType{}, fmt.Errorf("port %q is a list — %q must be a numeric index or %s, not a field name",
				ep.Port, fields[0], FanOutMarker)
		}
		base = *base.List
		fields = fields[1:]
	}
	if len(fields) > 0 && fields[0] == FanOutMarker {
		return schema.ParsedType{}, fmt.Errorf("port %q is %s, not a list — there is nothing for %s to iterate over",
			ep.Port, base, FanOutMarker)
	}
	if len(fields) == 0 {
		return base, nil
	}
	if base.Base != schema.PortJSON {
		return schema.ParsedType{}, fmt.Errorf("port %q is %s, not json — cannot access field %q",
			ep.Port, base, strings.Join(fields, "."))
	}
	if schemaFile == "" {
		return schema.ParsedType{}, fmt.Errorf("port %q has no schema: file to resolve field %q against",
			ep.Port, strings.Join(fields, "."))
	}
	return resolveFieldType(filepath.Join(rb.Nanobot.SourcePath, schemaFile), fields)
}

// resolveFieldType walks a JSON Schema file's `properties` tree following
// `fields`, returning the leaf's type as a ParsedType.
func resolveFieldType(schemaPath string, fields []string) (schema.ParsedType, error) {
	raw, err := os.ReadFile(schemaPath)
	if err != nil {
		return schema.ParsedType{}, fmt.Errorf("read schema %s: %w", schemaPath, err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return schema.ParsedType{}, fmt.Errorf("parse schema %s: %w", schemaPath, err)
	}
	node := doc
	for i, f := range fields {
		props, ok := node["properties"].(map[string]any)
		if !ok {
			return schema.ParsedType{}, fmt.Errorf("schema %s: %q has no properties to look up %q",
				schemaPath, strings.Join(fields[:i], "."), f)
		}
		next, ok := props[f].(map[string]any)
		if !ok {
			return schema.ParsedType{}, fmt.Errorf("schema %s: no property %q", schemaPath, f)
		}
		node = next
	}
	jsType, _ := node["type"].(string)
	return jsonSchemaTypeToPortType(jsType)
}

func jsonSchemaTypeToPortType(t string) (schema.ParsedType, error) {
	switch t {
	case "string":
		return schema.ParsedType{Base: schema.PortString}, nil
	case "boolean":
		return schema.ParsedType{Base: schema.PortBoolean}, nil
	case "object":
		return schema.ParsedType{Base: schema.PortJSON}, nil
	case "array":
		inner := schema.ParsedType{Base: schema.PortJSON}
		return schema.ParsedType{Base: "list", List: &inner}, nil
	case "integer", "number":
		// No numeric port type exists yet; treat as an opaque json scalar
		// rather than inventing a type the rest of the system doesn't know.
		return schema.ParsedType{Base: schema.PortJSON}, nil
	default:
		return schema.ParsedType{}, fmt.Errorf("unsupported JSON Schema type %q", t)
	}
}
