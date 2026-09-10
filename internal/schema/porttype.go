package schema

import (
	"fmt"
	"strings"
)

// ParsedType is a port type after parsing "list<...>" nesting. Base is one of
// the PortType constants (or PortJSON for an unrecognized inner type, which
// is treated permissively).
type ParsedType struct {
	Base PortType
	List *ParsedType // non-nil if this is list<List>
}

func (p ParsedType) String() string {
	if p.List != nil {
		return "list<" + p.List.String() + ">"
	}
	return string(p.Base)
}

var baseTypes = map[PortType]bool{
	PortString: true, PortDatetime: true, PortBoolean: true,
	PortJSON: true, PortFile: true, PortEvent: true,
}

// ParsePortType parses a port type string like "string", "list<json>", or
// "list<list<string>>".
func ParsePortType(s string) (ParsedType, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, "?") // optional-port marker some bots use inline
	if strings.HasPrefix(s, "list<") && strings.HasSuffix(s, ">") {
		inner := s[len("list<") : len(s)-1]
		innerType, err := ParsePortType(inner)
		if err != nil {
			return ParsedType{}, err
		}
		return ParsedType{Base: "list", List: &innerType}, nil
	}
	pt := PortType(s)
	if !baseTypes[pt] {
		return ParsedType{}, fmt.Errorf("unknown port type %q", s)
	}
	return ParsedType{Base: pt}, nil
}

// Assignable reports whether a value of type `from` may be snapped into a
// port of type `to`. The rule is strict nominal matching (including through
// list<...> nesting) — the planner is responsible for resolving dotted-path
// field access on a `json` output against its referenced schema *before*
// calling Assignable, so by the time we get here both sides are concrete
// scalar/list types, not "some field of some json blob".
func Assignable(from, to ParsedType) bool {
	if from.Base == "list" || to.Base == "list" {
		if from.Base != "list" || to.Base != "list" {
			return false
		}
		return Assignable(*from.List, *to.List)
	}
	if from.Base == to.Base {
		return true
	}
	// A json-typed output may snap into a json-typed input directly (no
	// field access) since both sides agree to carry an opaque object.
	return from.Base == PortJSON && to.Base == PortJSON
}
