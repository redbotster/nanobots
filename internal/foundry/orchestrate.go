package foundry

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/redbotster/nanobots/internal/contract"
	"github.com/redbotster/nanobots/internal/oneclaw"
	"github.com/redbotster/nanobots/internal/runner"
	"github.com/redbotster/nanobots/internal/schema"
)

const (
	defaultMaxIterations = 8
	defaultMaxWallClock  = 20 * time.Minute

	// reviewTimeout is far longer than runner's own 30-minute
	// approvalTimeout — approving a mid-swarm send blocks a live
	// automation the human is presumably watching; reviewing a freshly
	// authored bot is asynchronous and the human may reasonably want a day
	// to look it over before it decides anything.
	reviewTimeout = 24 * time.Hour
)

// Config is everything an Orchestrator needs, constructed identically in
// internal/daemon/daemon.go and cmd/nanobots/main.go — the same two places
// step.GoogleConfig and friends are already built twice today.
type Config struct {
	RepoRoot      string
	BotsDir       string
	WorkDir       string // e.g. ~/.nanobots/foundry — never inside RepoRoot
	AgentStateDir string
	OneClaw       *oneclaw.Client // nil is fine — see run()'s best-effort agent registration
	Agent         Agent
	MaxIterations int           // 0 => defaultMaxIterations
	MaxWallClock  time.Duration // 0 => defaultMaxWallClock
}

type Orchestrator struct {
	Config
}

// StartJob kicks off one foundry job in the background and returns
// immediately with the job in "running" status — the same
// fire-and-forget-then-poll/subscribe shape runner.Orchestrator.ExecuteSwarm
// already uses for a swarm run.
func (o *Orchestrator) StartJob(request, missingCapability string, suggestedInputs []schema.InputPort, suggestedOutputs []schema.OutputPort) (*Job, error) {
	if o.Agent == nil {
		return nil, fmt.Errorf("foundry: no coding agent configured")
	}
	job := NewJob(request, missingCapability)
	job.SetStatus(runner.StatusRunning)

	maxIter := o.MaxIterations
	if maxIter == 0 {
		maxIter = defaultMaxIterations
	}
	maxWall := o.MaxWallClock
	if maxWall == 0 {
		maxWall = defaultMaxWallClock
	}

	go o.run(job, suggestedInputs, suggestedOutputs, maxIter, maxWall)
	return job, nil
}

func (o *Orchestrator) run(job *Job, suggestedInputs []schema.InputPort, suggestedOutputs []schema.OutputPort, maxIter int, maxWall time.Duration) {
	worktreeDir := filepath.Join(o.WorkDir, job.ID, "worktree")
	branch := "foundry/" + job.ID

	existingIDs, err := existingBotIDs(o.BotsDir)
	if err != nil {
		o.fail(job, fmt.Errorf("list existing catalog: %w", err))
		return
	}
	idList := make([]string, 0, len(existingIDs))
	for id := range existingIDs {
		idList = append(idList, id)
	}

	job.Log("foundry", "setup", "creating a sandboxed worktree...")
	if err := createWorktree(o.RepoRoot, worktreeDir, branch); err != nil {
		o.fail(job, fmt.Errorf("create sandbox: %w", err))
		return
	}
	job.setWorktreePath(worktreeDir)

	// Best-effort only: this registration is for identity/observability
	// consistency with every other 1Claw-touching actor in this system —
	// see job.go's package doc for why it doesn't actually meter the
	// coding agent's real token spend, and why that means it's fine for
	// this to fail without failing the job.
	if o.OneClaw != nil {
		if _, _, err := o.OneClaw.EnsureAgent(o.AgentStateDir, "nanobots-foundry", oneclaw.CreateAgentRequest{
			ShroudEnabled: true,
			ShroudConfig:  &oneclaw.ShroudConfig{PIIPolicy: "redact", EnableSecretRedaction: true, DailyBudgetUSD: 5},
		}); err != nil {
			job.Log("foundry", "setup", "warning: couldn't register the foundry's 1Claw agent identity: %v", err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), maxWall)
	defer cancel()

	events := make(chan Event, 64)
	done := make(chan error, 1)
	go func() {
		done <- o.Agent.Run(ctx, worktreeDir, BriefInput{
			Request:           job.Request,
			MissingCapability: job.MissingCapability,
			SuggestedInputs:   suggestedInputs,
			SuggestedOutputs:  suggestedOutputs,
			ExistingBotIDs:    idList,
		}, events)
	}()

	agentErr, iterLimitHit := o.drainEvents(job, events, done, maxIter, cancel)
	if agentErr != nil {
		if iterLimitHit || ctx.Err() == context.DeadlineExceeded {
			job.setOutcome(OutcomeTimeout)
			o.fail(job, fmt.Errorf("the coding agent exceeded its budget: %w", agentErr))
			return
		}
		o.fail(job, fmt.Errorf("coding agent failed: %w", agentErr))
		return
	}

	botID, err := verifySandbox(worktreeDir, existingIDs)
	if err != nil {
		job.setOutcome(OutcomeSandboxViolation)
		o.fail(job, err)
		return
	}
	job.setBotID(botID)
	job.Log("foundry", "verify", "sandbox check passed — exactly one new bot directory: %s", botID)

	report, err := contract.RunConformance(filepath.Join(worktreeDir, "bots", botID), "")
	if err != nil || !report.OK() {
		job.setOutcome(OutcomeConformFailed)
		if err != nil {
			o.fail(job, fmt.Errorf("conformance check errored: %w", err))
		} else {
			o.fail(job, fmt.Errorf("bots/%s doesn't conform:\n%s", botID, report.String()))
		}
		return
	}
	job.setConformOK(true)
	job.Log("foundry", "verify", "bots/%s conforms", botID)

	preview, err := loadPreview(worktreeDir, botID)
	if err != nil {
		o.fail(job, fmt.Errorf("load the new bot's own definition: %w", err))
		return
	}
	job.setBotPreview(preview)

	approved, decidedBy, err := job.RequestApproval("foundry", "review-bot",
		fmt.Sprintf("A new bot, %q, was authored for: %s. Review it and approve to add it to the catalog.", botID, job.MissingCapability),
		"high", reviewTimeout)
	if err != nil {
		o.fail(job, err)
		return
	}
	if !approved {
		job.setOutcome(OutcomeRejected)
		job.Log("foundry", "review", "rejected by %s", decidedBy)
		job.SetStatus(runner.StatusFailed)
		return
	}
	job.Log("foundry", "review", "approved by %s", decidedBy)

	if err := promote(worktreeDir, o.BotsDir, botID); err != nil {
		o.fail(job, fmt.Errorf("promote: %w", err))
		return
	}
	if err := removeWorktree(o.RepoRoot, worktreeDir, branch); err != nil {
		job.Log("foundry", "cleanup", "warning: couldn't clean up the sandbox worktree: %v", err)
	}
	job.setOutcome(OutcomePromoted)
	job.Log("foundry", "done", "bots/%s is now part of the catalog", botID)
	job.SetStatus(runner.StatusSucceeded)
}

func (o *Orchestrator) fail(job *Job, err error) {
	job.Log("foundry", "error", "%v", err)
	job.SetError(err)
	job.SetStatus(runner.StatusFailed)
}

// drainEvents logs every Event as it arrives, counts conform attempts
// (surfaced by an Agent as a "tool" phase Event whose Msg starts with
// "conform"), and cancels the agent's context if that count exceeds
// maxIter — the enforced half of the "bounded loop" guarantee (the other
// half is the wall-clock context timeout the caller already set up).
// Returns the agent's final error (if any) and whether the iteration cap,
// specifically, was what ended it.
func (o *Orchestrator) drainEvents(job *Job, events <-chan Event, done <-chan error, maxIter int, cancel context.CancelFunc) (err error, iterLimitHit bool) {
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			job.Log("foundry", ev.Phase, "%s", ev.Msg)
			if ev.Phase == "tool" && strings.HasPrefix(ev.Msg, "conform") {
				if job.incrementIterations() > maxIter {
					iterLimitHit = true
					job.Log("foundry", "limit", "exceeded max conform attempts (%d) — stopping", maxIter)
					cancel()
				}
			}
		case agentErr := <-done:
			// Drain whatever's already buffered so the log doesn't miss
			// the agent's final lines.
			for {
				select {
				case ev, ok := <-events:
					if !ok {
						return agentErr, iterLimitHit
					}
					job.Log("foundry", ev.Phase, "%s", ev.Msg)
				default:
					return agentErr, iterLimitHit
				}
			}
		}
	}
}

func loadPreview(worktreeDir, botID string) (*BotPreview, error) {
	nb, err := schema.LoadNanobot(filepath.Join(worktreeDir, "bots", botID, "nanobot.yaml"))
	if err != nil {
		return nil, err
	}
	return &BotPreview{
		ID: botID, Name: nb.Metadata.Name, Version: nb.Metadata.Version,
		Description: nb.Metadata.Description, Harness: nb.Spec.Harness.Type,
		Tags: nb.Metadata.Tags, Services: nb.Spec.Services,
		Inputs: nb.Spec.Ports.Inputs, Outputs: nb.Spec.Ports.Outputs,
		Guardrails: nb.Spec.Guardrails,
	}, nil
}
