package schema

import (
	"reflect"
	"strings"
)

// jsonSchemaOf builds a JSON Schema (draft 2020-12 shaped) document for a Go
// type via reflection over its `json` struct tags. It's intentionally small —
// just enough to keep schemas/*.schema.json honest as the Go types evolve,
// not a general-purpose library. Fields tagged `json:"-"` are skipped; a
// field without `,omitempty` is treated as required.
func jsonSchemaOf(t reflect.Type) map[string]any {
	seen := map[reflect.Type]bool{}
	return schemaFor(t, seen)
}

func schemaFor(t reflect.Type, seen map[reflect.Type]bool) map[string]any {
	switch t.Kind() {
	case reflect.Ptr:
		return schemaFor(t.Elem(), seen)
	case reflect.String:
		return map[string]any{"type": "string"}
	case reflect.Bool:
		return map[string]any{"type": "boolean"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return map[string]any{"type": "integer"}
	case reflect.Float32, reflect.Float64:
		return map[string]any{"type": "number"}
	case reflect.Slice, reflect.Array:
		return map[string]any{"type": "array", "items": schemaFor(t.Elem(), seen)}
	case reflect.Map:
		return map[string]any{"type": "object", "additionalProperties": true}
	case reflect.Interface:
		// map[string]any / []any leaves — accept anything.
		return map[string]any{}
	case reflect.Struct:
		if seen[t] {
			// Guard against accidental recursion; none of our types are
			// self-referential today, but this keeps it safe if that changes.
			return map[string]any{"type": "object"}
		}
		seen[t] = true
		props := map[string]any{}
		var required []string
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			tag := f.Tag.Get("json")
			if tag == "-" || tag == "" {
				continue
			}
			parts := strings.Split(tag, ",")
			name := parts[0]
			omitempty := false
			for _, p := range parts[1:] {
				if p == "omitempty" {
					omitempty = true
				}
			}
			props[name] = schemaFor(f.Type, seen)
			if !omitempty {
				required = append(required, name)
			}
		}
		out := map[string]any{
			"type":       "object",
			"properties": props,
		}
		if len(required) > 0 {
			out["required"] = required
		}
		return out
	default:
		return map[string]any{}
	}
}

// NanobotJSONSchema returns the JSON Schema document for the Nanobot resource.
func NanobotJSONSchema() map[string]any {
	s := jsonSchemaOf(reflect.TypeOf(Nanobot{}))
	s["$schema"] = "https://json-schema.org/draft/2020-12/schema"
	s["$id"] = "https://nanobots.dev/schemas/nanobot.schema.json"
	s["title"] = "Nanobot"
	return s
}

// NanoswarmJSONSchema returns the JSON Schema document for the Nanoswarm resource.
func NanoswarmJSONSchema() map[string]any {
	s := jsonSchemaOf(reflect.TypeOf(Nanoswarm{}))
	s["$schema"] = "https://json-schema.org/draft/2020-12/schema"
	s["$id"] = "https://nanobots.dev/schemas/nanoswarm.schema.json"
	s["title"] = "Nanoswarm"
	return s
}
