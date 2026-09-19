package runner

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/redbotster/nanobots/internal/schema"
	"github.com/redbotster/nanobots/internal/step"
)

// Running a bot without a container, when the container was not protecting
// anything.
//
// A bot's steps are not user code. They are a fixed list, declared in its
// nanobot.yaml, executed by this repo's own interpreter — and every step
// that reaches the outside world (service.call, ai.generate, web.fetch,
// memory.*, approve, notify) already runs *in nanobotd* and is reached from
// the container by an HTTP callback. So for most of the catalog the
// container holds no credential, runs nothing we did not write, and exists
// to isolate a process whose only privileged act is to phone home.
//
// What it costs: pulling or building an image, starting a container,
// mounting three directories, and a callback round trip per step. On this
// machine that is most of a short bot's wall clock.
//
// What it is still needed for, and why this is a per-bot decision rather
// than a flag:
//
//   - openclaw bots drive a real headless Chromium, which is a browser
//     executing pages we did not write. That is exactly the thing a sandbox
//     is for, and it keeps its container.
//   - a future `harness: agent` loop decides its own actions at runtime.
//     The moment a bot's behaviour stops being a fixed declared list, the
//     isolation argument comes back.
//
// One honest difference from the container path, stated here because it is
// the kind of thing that should not be discovered later: `docker kill` ends
// a hung bot outright, and nothing here can. step.Interpret takes no
// context and neither do the Deps methods, so a timeout stops the run
// *waiting* but cannot stop the work. In practice each blocking call
// carries its own deadline — the HTTP clients, the LLM client, the approval
// gate — and this ceiling is the backstop rather than the only bound.
//
// The upside of that same property: a bot parked on an approval in-process
// holds a goroutine, not a container. On this machine, approvals nobody
// answered burned 27 hours of container time.

// runsInProcess reports whether this bot can skip its container, and says
// why not when it cannot, so the run log can explain itself.
//
// instanceOverride is this swarm's own execution: on the bot instance
// (schema.BotRef.Execution), which wins over the bot's own catalog default
// (schema.Harness.Execution) when both are set — the swarm author's call
// on a bot they know is fine in-process everywhere else beats the bot
// author's blanket default. A caller with no swarm context (warmimages.go
// scanning the whole catalog) passes "". CheckExecution has already
// rejected any value here other than "" or "container" by the time a run
// reaches this far, so those are the only two this checks for.
func runsInProcess(nb *schema.Nanobot, instanceOverride string) (bool, string) {
	switch {
	case instanceOverride == "container":
		return false, "execution: container (forced by this swarm)"
	case nb.Spec.Harness.Execution == "container":
		return false, "execution: container (forced by the bot)"
	}
	if needsBrowser(nb) {
		return false, "it renders with a real headless browser"
	}
	if nb.Spec.Harness.Type == "openclaw" {
		return false, "it declares the openclaw harness"
	}
	return true, ""
}

// interpretInProcess runs one bot's steps in this process and writes its
// outputs where collectOutputs will find them — the same directory, the
// same file shapes, via the same step.WriteOutput the container agent uses.
//
// deps is the host-side Deps the callback registry would have handed the
// container anyway, so a bot cannot tell the difference by behaviour: the
// same fixtures, the same live clients, the same approval queue.
func interpretInProcess(
	run *Run, botID string, nb *schema.Nanobot,
	inputs, swarmVars map[string]any,
	deps step.Deps, blobs step.BlobStore, runDir string, maxRuntime time.Duration,
) error {
	// Log lines arrive as the steps happen rather than being tailed out of
	// a file, which is the one way in-process is observably better: there
	// is no poll interval between a step finishing and the run log showing
	// it.
	deps = &liveLogDeps{Deps: deps, run: run, bot: botID}

	type outcome struct {
		res *step.Result
		err error
	}
	done := make(chan outcome, 1)
	go func() {
		res, err := step.Interpret(nb, inputs, swarmVars, deps)
		done <- outcome{res, err}
	}()

	if maxRuntime <= 0 {
		maxRuntime = time.Hour // a bot with no declared ceiling still gets one
	}
	var got outcome
	select {
	case got = <-done:
	case <-time.After(maxRuntime):
		return fmt.Errorf("bot %s exceeded %s and was abandoned", nb.Metadata.Name, maxRuntime)
	case <-run.Context().Done():
		if run.WasStoppedByUser() {
			return ErrStopped
		}
		return run.Context().Err()
	}
	if got.err != nil {
		return fmt.Errorf("bot %s: %w", nb.Metadata.Name, got.err)
	}
	if got.res.Stopped {
		// Not a failure and not an empty answer: this bot looked, found
		// nothing new, and wrote nothing. runDAG turns it into a skip
		// for everything downstream. See the stop.if step.
		return &NothingToDoError{Bot: nb.Metadata.Name, Reason: got.res.StopReason}
	}

	outDir := filepath.Join(runDir, "outputs")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	for name, val := range got.res.Outputs {
		if err := step.WriteOutput(outDir, blobs, name, val); err != nil {
			return fmt.Errorf("write output %q: %w", name, err)
		}
	}
	return nil
}

// liveLogDeps forwards the interpreter's per-step log lines straight into
// the run, which is what the container path achieves by writing log.jsonl
// and having the daemon tail it.
type liveLogDeps struct {
	step.Deps
	run *Run
	bot string
}

// %s rather than msg as the format: a step message can contain a percent
// sign (a rendered template, a URL with an escape) and Log formats.
func (d *liveLogDeps) StreamLog(stepName, msg string) { d.run.Log(d.bot, stepName, "%s", msg) }
