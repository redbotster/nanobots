// Package foundry is the escalation path behind the AI composer
// (internal/api/compose.go): when the composer declares that no
// combination of the existing catalog can satisfy a request, a foundry Job
// hands the gap to a sandboxed coding-agent subprocess that authors a
// brand-new bot from scratch, self-tests it via the real conformance
// runner until it passes, and only lets it join the real catalog once a
// human explicitly approves it. Composer, foundry, and harness stay
// deliberately distinct concepts: the composer assembles swarms from known
// bots, the foundry authors brand-new ones, and a harness (bare/openclaw)
// is how a bot runs — none of that changes here.
//
// A real, disclosed gap: the coding agent's own token spend isn't actually
// metered through 1Claw/Shroud — internal/oneclaw.ShroudClient.Chat is a
// single-shot, one-message-in/one-message-out proxy, not a multi-turn,
// tool-using session an external CLI agent could sit behind. A Job still
// registers a named 1Claw agent for consistency/observability with every
// other actor in this system (see Orchestrator.run), but the real,
// enforced spend guard is the wall-clock and iteration caps on the
// subprocess itself, not a Shroud daily budget.
package foundry

import (
	"sync"

	"github.com/redbotster/nanobots/internal/runner"
	"github.com/redbotster/nanobots/internal/schema"
)

// Outcome distinguishes *why* a job ended, since runner.RunStatus's
// "failed" alone can't tell "the agent's bot never conformed" apart from
// "a human looked at a passing bot and said no" — very different things to
// show a human reviewing job history.
type Outcome string

const (
	OutcomePromoted         Outcome = "promoted"
	OutcomeRejected         Outcome = "rejected"
	OutcomeConformFailed    Outcome = "conform_failed"
	OutcomeSandboxViolation Outcome = "sandbox_violation"
	OutcomeTimeout          Outcome = "timeout"
)

// BotPreview mirrors internal/api.BotSummary field-for-field, duplicated
// (not imported) to avoid an internal/api <-> internal/foundry import
// cycle — internal/api's own foundry handlers map this 1:1 onto the wire
// so a job's preview is typed identically to any other catalog bot.
type BotPreview struct {
	ID          string
	Name        string
	Version     string
	Description string
	Harness     string
	Tags        []string
	Services    []schema.Service
	Inputs      []schema.InputPort
	Outputs     []schema.OutputPort
	Guardrails  schema.Guardrails
}

// Job is one foundry run. It embeds *runner.Run to reuse its logging,
// SSE pub-sub, and approval-gate machinery verbatim — none of that is
// Docker- or swarm-specific (see run.go), so there's no reason to
// duplicate it. runner.RunStatus's five values map cleanly onto a job's
// lifecycle: "running" covers both authoring and conforming,
// "awaiting_approval" *is* the human review gate (RequestApproval already
// flips to/from it), and "succeeded"/"failed" cover promoted vs. anything
// else — Outcome (accessed via Outcome()/setOutcome) carries the
// finer-grained why.
//
// The fields below are mutated by the job's background goroutine while an
// HTTP handler polls them concurrently (GET /api/foundry/{id}), so — like
// runner.Run's own Status/Error — they're guarded by a mutex and reached
// through accessor methods, not read/written directly.
type Job struct {
	*runner.Run

	Request           string
	MissingCapability string

	mu           sync.Mutex
	botID        string
	worktreePath string
	iterations   int
	conformOK    bool
	outcome      Outcome
	botPreview   *BotPreview
}

func NewJob(request, missingCapability string) *Job {
	return &Job{
		Run:               runner.NewRun("foundry: " + missingCapability),
		Request:           request,
		MissingCapability: missingCapability,
	}
}

func (j *Job) BotID() string {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.botID
}

func (j *Job) setBotID(id string) {
	j.mu.Lock()
	j.botID = id
	j.mu.Unlock()
}

func (j *Job) WorktreePath() string {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.worktreePath
}

func (j *Job) setWorktreePath(p string) {
	j.mu.Lock()
	j.worktreePath = p
	j.mu.Unlock()
}

func (j *Job) Iterations() int {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.iterations
}

// incrementIterations returns the new count, so a caller can compare
// against a cap in one call without a separate read.
func (j *Job) incrementIterations() int {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.iterations++
	return j.iterations
}

func (j *Job) ConformOK() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.conformOK
}

func (j *Job) setConformOK(ok bool) {
	j.mu.Lock()
	j.conformOK = ok
	j.mu.Unlock()
}

func (j *Job) Outcome() Outcome {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.outcome
}

func (j *Job) setOutcome(o Outcome) {
	j.mu.Lock()
	j.outcome = o
	j.mu.Unlock()
}

func (j *Job) BotPreview() *BotPreview {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.botPreview
}

func (j *Job) setBotPreview(p *BotPreview) {
	j.mu.Lock()
	j.botPreview = p
	j.mu.Unlock()
}
