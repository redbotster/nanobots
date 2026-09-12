package api

import (
	"bytes"
	"fmt"

	"gopkg.in/yaml.v3"
)

// The visual builder models four things about a swarm: its name, its
// description, its bots and its snaps. A swarm file holds considerably more
// — a cron trigger, swarm vars, guardrail defaults (PII policy, injection
// threshold, daily budget), a deploy target, an owner, and header comments
// explaining every simplification the author made.
//
// Saving used to marshal the builder's four-field model straight over the
// file. Opening bookkeeping-assistant, changing nothing, and pressing "Save
// changes" replaced its `trigger: {cron, "0 15 * * 5", America/Chicago}`
// with `trigger: {type: manual}` — the swarm silently stopped firing
// forever — and dropped its vars, its guardrails and its comments with it.
// From a button labelled "Save changes", with no warning and no undo.
//
// mergeIntoExistingSwarm edits only the four fields the builder owns,
// in place, in the original document. Everything else survives byte for
// byte, comments included, because the edit happens on a yaml.Node tree
// rather than a round-tripped struct.

// mergeIntoExistingSwarm returns existing with only the builder-owned
// fields replaced. Every other key, and every comment, is preserved.
func mergeIntoExistingSwarm(existing []byte, name, description string, bots []builderBotRef, snaps []builderSnap) ([]byte, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(existing, &doc); err != nil {
		return nil, fmt.Errorf("parse existing swarm: %w", err)
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return nil, fmt.Errorf("existing swarm is not a YAML document")
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("existing swarm is not a mapping")
	}

	metadata := ensureMapping(root, "metadata")
	setScalar(metadata, "name", name)
	setScalar(metadata, "description", description)

	spec := ensureMapping(root, "spec")

	// Bots and snaps are rebuilt wholesale — they *are* what the builder
	// edits, so there is nothing to preserve inside them. Comments attached
	// to individual bot entries are the one real casualty, and that's
	// unavoidable when the list itself is the thing being replaced.
	botsNode, err := toNode(schemaBots(bots))
	if err != nil {
		return nil, err
	}
	setNode(spec, "bots", botsNode)

	if len(snaps) > 0 {
		snapsNode, err := toNode(schemaSnaps(snaps))
		if err != nil {
			return nil, err
		}
		setNode(spec, "snaps", snapsNode)
	} else {
		deleteKey(spec, "snaps")
	}

	// Encode with a 2-space indent to match how these files are written by
	// hand — yaml.Marshal defaults to 4 and would re-indent the entire
	// document on every save, burying the actual change in churn.
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return nil, fmt.Errorf("re-encode swarm: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("re-encode swarm: %w", err)
	}
	return buf.Bytes(), nil
}

func schemaBots(bots []builderBotRef) []any {
	out := make([]any, 0, len(bots))
	for _, b := range bots {
		out = append(out, b.toSchema())
	}
	return out
}

func schemaSnaps(snaps []builderSnap) []any {
	out := make([]any, 0, len(snaps))
	for _, s := range snaps {
		out = append(out, s.toSchema())
	}
	return out
}

func toNode(v any) (*yaml.Node, error) {
	var n yaml.Node
	if err := n.Encode(v); err != nil {
		return nil, fmt.Errorf("encode: %w", err)
	}
	return &n, nil
}

// findKey returns the index of key's *value* node in a mapping's Content,
// or -1. yaml.Node mappings store alternating key/value nodes.
func findKey(m *yaml.Node, key string) int {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return i + 1
		}
	}
	return -1
}

func setNode(m *yaml.Node, key string, value *yaml.Node) {
	if i := findKey(m, key); i >= 0 {
		// Keep the existing value node's comments — they belong to this
		// key, not to the value we're replacing.
		value.HeadComment = m.Content[i].HeadComment
		value.LineComment = m.Content[i].LineComment
		value.FootComment = m.Content[i].FootComment
		m.Content[i] = value
		return
	}
	m.Content = append(m.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		value)
}

func setScalar(m *yaml.Node, key, value string) {
	if i := findKey(m, key); i >= 0 {
		m.Content[i].Kind = yaml.ScalarNode
		m.Content[i].Tag = "!!str"
		m.Content[i].Value = value
		m.Content[i].Content = nil
		return
	}
	m.Content = append(m.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value})
}

func deleteKey(m *yaml.Node, key string) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content = append(m.Content[:i], m.Content[i+2:]...)
			return
		}
	}
}

func ensureMapping(m *yaml.Node, key string) *yaml.Node {
	if i := findKey(m, key); i >= 0 && m.Content[i].Kind == yaml.MappingNode {
		return m.Content[i]
	}
	child := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	setNode(m, key, child)
	return child
}
