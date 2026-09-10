package schema

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// LoadNanobot reads and parses a nanobot.yaml file. SourcePath is set to the
// file's containing directory so callers can resolve bot.md/prompts/etc.
func LoadNanobot(path string) (*Nanobot, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var nb Nanobot
	if err := yaml.Unmarshal(raw, &nb); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if nb.Kind != "" && nb.Kind != "Nanobot" {
		return nil, fmt.Errorf("%s: kind %q, expected Nanobot", path, nb.Kind)
	}
	if nb.Metadata.Name == "" {
		return nil, fmt.Errorf("%s: metadata.name is required", path)
	}
	nb.SourcePath = filepath.Dir(path)
	return &nb, nil
}

// LoadNanoswarm reads and parses a nanoswarm.yaml file.
func LoadNanoswarm(path string) (*Nanoswarm, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var sw Nanoswarm
	if err := yaml.Unmarshal(raw, &sw); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if sw.Kind != "" && sw.Kind != "Nanoswarm" {
		return nil, fmt.Errorf("%s: kind %q, expected Nanoswarm", path, sw.Kind)
	}
	if sw.Metadata.Name == "" {
		return nil, fmt.Errorf("%s: metadata.name is required", path)
	}
	sw.SourcePath = filepath.Dir(path)
	return &sw, nil
}
