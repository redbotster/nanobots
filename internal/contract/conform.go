// Package contract implements the bot I/O contract and its conformance test
// runner. The contract itself (see docs/bot-contract.md) is: a bot receives
// its resolved inputs as a JSON envelope, produces its declared output
// ports, and exits 0. This runner drives that contract with DemoDeps (no
// network, no Docker) so `nanobots conform ./bots/<id>` and `go test` can
// both run it fast and offline.
package contract

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/redbotster/nanobots/internal/schema"
	"github.com/redbotster/nanobots/internal/step"
)

// Report is the outcome of running one bot's conformance fixtures.
type Report struct {
	BotName string
	Log     []step.LogLine
	Outputs map[string]any
	Errors  []string
}

func (r *Report) OK() bool { return len(r.Errors) == 0 }

func (r *Report) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "conform: %s\n\n", r.BotName)
	for _, l := range r.Log {
		fmt.Fprintf(&b, "  [%s] %s\n", l.Step, l.Msg)
	}
	if len(r.Outputs) > 0 {
		b.WriteString("\noutputs:\n")
		names := make([]string, 0, len(r.Outputs))
		for name := range r.Outputs {
			names = append(names, name)
		}
		for _, name := range names {
			fmt.Fprintf(&b, "  %s: %s\n", name, describe(r.Outputs[name]))
		}
	}
	if r.OK() {
		b.WriteString("\nconform OK — every declared output port was produced with the declared type.\n")
	} else {
		b.WriteString("\nconform FAILED:\n")
		for _, e := range r.Errors {
			fmt.Fprintf(&b, "  - %s\n", e)
		}
	}
	return b.String()
}

func describe(v any) string {
	switch vv := v.(type) {
	case step.FileValue:
		return fmt.Sprintf("file %s (%s)", vv.URI, vv.Mime)
	case map[string]any, []any:
		b, _ := json.Marshal(vv)
		return truncate(string(b), 120)
	default:
		return fmt.Sprint(vv)
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// RunConformance loads botDir/nanobot.yaml, feeds it fixturesDir/inputs.json
// (default botDir/fixtures/inputs.json), and runs it against DemoDeps rooted
// at fixturesDir. The returned error is only for setup problems (bad bot
// directory, missing fixtures); a bot that runs but fails its contract is
// reported via Report.Errors / Report.OK(), the same pattern as planner.Plan.
func RunConformance(botDir, fixturesDir string) (*Report, error) {
	nb, err := schema.LoadNanobot(filepath.Join(botDir, "nanobot.yaml"))
	if err != nil {
		return nil, err
	}
	if fixturesDir == "" {
		fixturesDir = filepath.Join(botDir, "fixtures")
	}
	inputsPath := filepath.Join(fixturesDir, "inputs.json")
	raw, err := os.ReadFile(inputsPath)
	if err != nil {
		return nil, fmt.Errorf("read %s (every bot needs a fixtures/inputs.json for conformance): %w", inputsPath, err)
	}
	var inputs map[string]any
	if err := json.Unmarshal(raw, &inputs); err != nil {
		return nil, fmt.Errorf("parse %s: %w", inputsPath, err)
	}
	// The bot contract says /run/inputs.json arrives "already-defaulted,
	// already-validated", and the runner does exactly that
	// (internal/runner/wiring.go). Conformance did not, so every optional
	// input a fixture left out reached the bot as nothing at all, and
	// conformance was proving the bot honours its contract under inputs no
	// real run would ever hand it.
	//
	// Found by a bot that passed: newsletter-drafter declares `to` with a
	// default of me@example.com and its fixture omits it, so `{{inputs.to}}`
	// resolved empty and it drafted an email with no recipient — green, for
	// as long as nothing looked at what went into the call.
	for _, p := range nb.Spec.Ports.Inputs {
		if _, ok := inputs[p.Name]; ok {
			continue
		}
		if p.Default != "" {
			inputs[p.Name] = step.ResolveTemplateValue(p.Default, map[string]any{})
			continue
		}
		if p.Required {
			return nil, fmt.Errorf("%s: required input %q is missing from %s", nb.Metadata.Name, p.Name, inputsPath)
		}
	}

	blobDir, err := os.MkdirTemp("", "nanobots-conform-blobs-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(blobDir)
	blobs, err := step.NewFSBlobStore(blobDir)
	if err != nil {
		return nil, err
	}

	// A `file`-typed input whose value in inputs.json is a placeholder
	// reference (nothing writes real blob content there — most bots never
	// read the bytes, e.g. drive-save just forwards the reference to a
	// service.call) won't resolve for a bot that actually needs the file's
	// *content*, like ai.generate's file-input-to-text resolution
	// (resolveFileInputAsText in internal/step/interpret.go). For those,
	// fixtures/<port>.content provides real bytes to seed the blob store
	// with, so conformance exercises the genuine content, not a stand-in.
	for _, p := range nb.Spec.Ports.Inputs {
		if p.Type != "file" {
			continue
		}
		if _, ok := inputs[p.Name]; !ok {
			continue
		}
		content, err := os.ReadFile(filepath.Join(fixturesDir, p.Name+".content"))
		if err != nil {
			continue // no real-content fixture for this port; leave inputs.json's value as-is
		}
		fv, err := blobs.Write(content, "text/plain")
		if err != nil {
			return nil, fmt.Errorf("seed blob store for input %q: %w", p.Name, err)
		}
		inputs[p.Name] = map[string]any{"uri": fv.URI, "mime": fv.Mime}
	}

	deps := step.NewDemoDeps(fixturesDir, blobs)
	// Conformance checks that a `file` port was produced with the right
	// shape, not that it's byte-for-byte a real PDF — real rendering is
	// exercised directly against the harness Docker images instead (see
	// docs/harnesses.md), so this stays hermetic and never depends on
	// whatever state the host's own Chrome happens to be in.
	deps.SkipRealRender = true

	report := &Report{BotName: nb.Metadata.Name}
	result, err := step.Interpret(nb, inputs, nil, deps)
	if err != nil {
		report.Errors = append(report.Errors, err.Error())
		if result != nil {
			report.Log = result.Log
		}
		return report, nil
	}
	report.Log = result.Log
	report.Outputs = result.Outputs
	return report, nil
}
