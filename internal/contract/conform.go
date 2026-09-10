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
	for _, p := range nb.Spec.Ports.Inputs {
		if p.Required {
			if _, ok := inputs[p.Name]; !ok {
				return nil, fmt.Errorf("%s: required input %q is missing from %s", nb.Metadata.Name, p.Name, inputsPath)
			}
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
	deps := step.NewDemoDeps(fixturesDir, blobs)
	// Conformance checks that a `file` port was produced with the right
	// shape, not that it's byte-for-byte a real PDF — real rendering is
	// exercised directly against the harness Docker images instead (see
	// docs/harnesses.md), so this stays hermetic and never depends on
	// whatever state the host's own Chrome happens to be in.
	deps.SkipRealRender = true

	report := &Report{BotName: nb.Metadata.Name}
	result, err := step.Interpret(nb, inputs, deps)
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
