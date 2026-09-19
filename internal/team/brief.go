package team

import (
	"fmt"
	"strings"
)

// TaskInput is one task handed to a Team member.
type TaskInput struct {
	Role string
	Task string
	// Engine selects the coding-agent CLI. "" defaults to EngineClaude.
	Engine Engine
}

// BuildBrief assembles the prompt for one Team task. Unlike a foundry
// job's brief (docs/bot-contract.md, two worked bots, and a hard boundary
// pinning the agent to exactly one new directory), a Team member's job is
// general on purpose — it's a role on a team, not a single-capability
// generator — so this brief states the safety model instead of a
// filesystem fence, and otherwise gets out of the way: workDir is a real
// worktree of this repo, so CLAUDE.md, docs/, and the bot catalog are
// already sitting there for the agent to read like any other contributor
// would.
func BuildBrief(in TaskInput, containerBinPath string) string {
	var b strings.Builder

	fmt.Fprintf(&b, "You are the %s on this nanobots team. This is your own persistent workspace — a real git worktree of the nanobots repo, on branch team/%s — not a one-shot sandbox that disappears when you're done. It carries your own commit history between tasks: `git log` before starting is how you find out what you (or an earlier task) already did here.\n\n", in.Role, in.Role)

	fmt.Fprintf(&b, "Your task: %s\n\n", in.Task)

	b.WriteString("The safety model, stated plainly because it's easy to mistake for a restriction placed on you specifically: nothing you write or edit here takes effect against a real account, sends anything, spends anything, or runs on a schedule until a human explicitly runs it or approves it. That is exactly as true of a human editing this same repo by hand — you are not more constrained than a person contributing here, and you are not less constrained either. Working within that is the job, not an obstacle to route around. If a task seems to need bypassing it, that's a sign to say so and stop, not to find a way around it.\n\n")

	fmt.Fprintf(&b, "The nanobots CLI is at %s (built from this workspace's own source, read-only). Useful commands: `%s plan -f <swarm.yaml>` to type-check a swarm before anything runs, `%s conform bots/<id>` to self-test a bot against its own fixtures, `%s run -f <swarm.yaml>` to run one to completion in demo mode and see its log. Read docs/anatomy.md first if you haven't built a nanobot or nanoswarm before — it's the concept doc for what a bot and a swarm actually look like as files.\n\n", containerBinPath, containerBinPath, containerBinPath, containerBinPath)

	b.WriteString("This repo's own CLAUDE.md, in its root, is how work here gets done — house voice, the verification pass, docs-ship-with-the-change. Read it before writing anything; it applies to you exactly as it applies to any other contributor working in this tree.\n\n")

	b.WriteString("When the task is done, say plainly what you changed and what's left, the same way you'd end any other piece of work. Leave your changes committed on your own branch (team/" + in.Role + ") if they're in a state worth keeping — nothing merges into main without a human doing that themselves.\n")

	return b.String()
}
