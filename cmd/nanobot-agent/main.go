// Command nanobot-agent is the container entrypoint every harness image
// runs: it reads a bot's nanobot.yaml and its resolved inputs, executes the
// bot contract (see docs/bot-contract.md), and writes the declared outputs.
// This is the "any harness that can read a file and write a file can be a
// nanobot" contract from NANOBOTS-BLUEPRINT.md §3.1, made concrete.
//
// It never holds a real 1Claw credential: when NANOBOTS_CALLBACK_URL is set,
// every credentialed step (service.call, ai.generate, memory, approve,
// notify) is a callback to nanobotd, authenticated with a random per-run
// token that's meaningless outside that one run (see internal/step.RemoteDeps
// and internal/runner). Without a callback URL configured, it runs against
// local DemoDeps instead — useful for testing a harness image standalone.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/redbotster/nanobots/internal/schema"
	"github.com/redbotster/nanobots/internal/step"
)

func env(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "nanobot-agent: error:", err)
		os.Exit(1)
	}
}

func run() error {
	botDir := env("NANOBOT_DIR", "/bot")
	runDir := env("NANOBOTS_RUN_DIR", "/run")

	nb, err := schema.LoadNanobot(filepath.Join(botDir, "nanobot.yaml"))
	if err != nil {
		return fmt.Errorf("load nanobot.yaml: %w", err)
	}

	inputsRaw, err := os.ReadFile(filepath.Join(runDir, "inputs.json"))
	if err != nil {
		return fmt.Errorf("read inputs.json: %w", err)
	}
	var inputs map[string]any
	if err := json.Unmarshal(inputsRaw, &inputs); err != nil {
		return fmt.Errorf("parse inputs.json: %w", err)
	}

	blobDir := env("NANOBOTS_BLOB_DIR", "/tmp/nanobots-blobs")
	blobs, err := step.NewFSBlobStore(blobDir)
	if err != nil {
		return fmt.Errorf("init blob store: %w", err)
	}

	deps := buildDeps(botDir, blobs)

	result, err := step.Interpret(nb, inputs, deps)
	if err != nil {
		writeLog(runDir, result)
		return fmt.Errorf("bot %s: %w", nb.Metadata.Name, err)
	}
	writeLog(runDir, result)

	outDir := filepath.Join(runDir, "outputs")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("create outputs dir: %w", err)
	}
	for name, val := range result.Outputs {
		if err := writeOutput(outDir, blobs, name, val); err != nil {
			return fmt.Errorf("write output %q: %w", name, err)
		}
	}
	return nil
}

func buildDeps(botDir string, blobs step.BlobStore) step.Deps {
	if callbackURL := os.Getenv("NANOBOTS_CALLBACK_URL"); callbackURL != "" {
		return step.NewRemoteDeps(callbackURL, os.Getenv("NANOBOTS_RUN_TOKEN"), blobs)
	}
	return step.NewDemoDeps(filepath.Join(botDir, "fixtures"), blobs)
}

// writeOutput writes one output port's value under outDir, per the bot
// contract: a `file` port writes raw bytes to <port> plus a <port>.mime
// sidecar; everything else is JSON-encoded to <port>.json.
func writeOutput(outDir string, blobs step.BlobStore, name string, val any) error {
	if fv, ok := val.(step.FileValue); ok {
		data, err := blobs.Read(fv.URI)
		if err != nil {
			return fmt.Errorf("read blob %s: %w", fv.URI, err)
		}
		if err := os.WriteFile(filepath.Join(outDir, name), data, 0o644); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(outDir, name+".mime"), []byte(fv.Mime), 0o644)
	}
	b, err := json.Marshal(val)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(outDir, name+".json"), b, 0o644)
}

// writeLog appends the run's step log as JSON lines to <run>/log.jsonl —
// nanobotd tails this (or the equivalent stdout stream) for the WebUI's live
// run log. Written even on failure, since a partial log is exactly what a
// human debugging the failure needs to see.
func writeLog(runDir string, result *step.Result) {
	if result == nil {
		return
	}
	f, err := os.OpenFile(filepath.Join(runDir, "log.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, line := range result.Log {
		enc.Encode(map[string]string{"step": line.Step, "msg": line.Msg})
	}
}
