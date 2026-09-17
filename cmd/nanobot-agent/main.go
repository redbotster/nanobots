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
	"sync"

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
	scratch, err := step.NewFSBlobStore(blobDir)
	if err != nil {
		return fmt.Errorf("init blob store: %w", err)
	}
	// Reads fall back to nanobotd's own store, mounted read-only, so a bot
	// can read a file an earlier bot in the swarm produced. Writes never go
	// there. See step.FallbackBlobStore.
	var blobs step.BlobStore = scratch
	if ro := os.Getenv("NANOBOTS_BLOB_READONLY_DIR"); ro != "" {
		if upstream, err := step.NewFSBlobStore(ro); err == nil {
			blobs = &step.FallbackBlobStore{Primary: scratch, Fallback: upstream}
		}
	}

	deps := buildDeps(botDir, blobs)

	// Stream each step to log.jsonl as it happens, so nanobotd can tail it
	// and the run log is live while this bot is still working. Before this,
	// every line was buffered here and written once on exit: a bot that
	// took forty seconds showed "starting" and then nothing at all until it
	// finished, which is not what "watch it run, live" means.
	//
	// Falling back to the end-of-run write if the file will not open: a log
	// that arrives late beats no log.
	streaming := newLogStream(runDir)
	if streaming != nil {
		defer streaming.Close()
		deps = &streamingDeps{Deps: deps, stream: streaming}
	}

	swarmVars, err := readSwarmVars(runDir)
	if err != nil {
		return fmt.Errorf("read swarm_vars.json: %w", err)
	}

	result, err := step.Interpret(nb, inputs, swarmVars, deps)
	if err != nil {
		if streaming == nil {
			writeLog(runDir, result)
		}
		return fmt.Errorf("bot %s: %w", nb.Metadata.Name, err)
	}
	if streaming == nil {
		writeLog(runDir, result)
	}

	outDir := filepath.Join(runDir, "outputs")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	if result.Stopped {
		// Stopped on purpose, with nothing to hand on. Exit 0 and no
		// outputs is indistinguishable from a bot that forgot to write
		// any, so leave the reason where the runner looks for it — see
		// runner.NothingToDoMarker and docs/bot-contract.md.
		return os.WriteFile(filepath.Join(outDir, ".nothing-to-do"),
			[]byte(result.StopReason), 0o644)
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("create outputs dir: %w", err)
	}
	for name, val := range result.Outputs {
		if err := step.WriteOutput(outDir, blobs, name, val); err != nil {
			return fmt.Errorf("write output %q: %w", name, err)
		}
	}
	return nil
}

// readSwarmVars loads /run/swarm_vars.json if the runner wrote one — a bot
// run outside any swarm (e.g. a standalone `nanobots run` on a single bot,
// or conformance testing) simply has none, which isn't an error.
func readSwarmVars(runDir string) (map[string]any, error) {
	raw, err := os.ReadFile(filepath.Join(runDir, "swarm_vars.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var vars map[string]any
	if err := json.Unmarshal(raw, &vars); err != nil {
		return nil, err
	}
	return vars, nil
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

// logStream appends one JSON line per step to <run>/log.jsonl, flushing
// each one, so a reader outside the container sees a step the moment it
// completes rather than when the container exits.
type logStream struct {
	mu sync.Mutex
	f  *os.File
}

func newLogStream(runDir string) *logStream {
	f, err := os.OpenFile(filepath.Join(runDir, "log.jsonl"),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil
	}
	return &logStream{f: f}
}

func (s *logStream) write(step, msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, err := json.Marshal(map[string]string{"step": step, "msg": msg})
	if err != nil {
		return
	}
	// One Write for the whole line including its newline. A reader tailing
	// this file must never see half a line, and two writes could be split.
	if _, err := s.f.Write(append(raw, '\n')); err != nil {
		return
	}
	// Sync, because the point is that someone else reads this now. Cheap
	// at a handful of lines per bot.
	_ = s.f.Sync()
}

func (s *logStream) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.f.Close()
}

// streamingDeps is the bot's real Deps plus step.LogStreamer, which is how
// the interpreter knows to hand over each line as it happens.
type streamingDeps struct {
	step.Deps
	stream *logStream
}

func (d *streamingDeps) StreamLog(step, msg string) { d.stream.write(step, msg) }
