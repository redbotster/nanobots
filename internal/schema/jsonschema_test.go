package schema

import "testing"

func TestNanobotJSONSchemaHasCoreFields(t *testing.T) {
	s := NanobotJSONSchema()
	if s["title"] != "Nanobot" {
		t.Fatalf("title = %v, want Nanobot", s["title"])
	}
	props, ok := s["properties"].(map[string]any)
	if !ok {
		t.Fatal("properties is not a map")
	}
	for _, field := range []string{"apiVersion", "kind", "metadata", "spec"} {
		if _, ok := props[field]; !ok {
			t.Errorf("missing top-level property %q", field)
		}
	}
	spec, ok := props["spec"].(map[string]any)
	if !ok {
		t.Fatal("spec property is not an object schema")
	}
	specProps, ok := spec["properties"].(map[string]any)
	if !ok {
		t.Fatal("spec.properties is not a map")
	}
	for _, field := range []string{"harness", "ports", "steps", "services", "guardrails"} {
		if _, ok := specProps[field]; !ok {
			t.Errorf("missing spec property %q", field)
		}
	}
}

func TestNanoswarmJSONSchemaHasCoreFields(t *testing.T) {
	s := NanoswarmJSONSchema()
	if s["title"] != "Nanoswarm" {
		t.Fatalf("title = %v, want Nanoswarm", s["title"])
	}
	props := s["properties"].(map[string]any)
	spec := props["spec"].(map[string]any)
	specProps := spec["properties"].(map[string]any)
	for _, field := range []string{"bots", "snaps", "trigger", "deploy"} {
		if _, ok := specProps[field]; !ok {
			t.Errorf("missing spec property %q", field)
		}
	}
}
