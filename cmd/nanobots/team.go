package main

import (
	"context"
	"fmt"

	"github.com/redbotster/nanobots/internal/foundry"
	"github.com/redbotster/nanobots/internal/oneclaw"
	"github.com/redbotster/nanobots/internal/team"
)

// `nanobots team` — give a Team member (a persistent, role-scoped coding
// agent; see context/TEAM-LAB-DESIGN.md) one task, in this terminal.
//
// This is deliberately a CLI command and not yet an API endpoint or a Lab
// tab — the design doc's own "Proposed next step" is to prove a Team
// agent can do real work through the ordinary approval gate before
// building a chat surface on top of it. `nanobots team run` is that
// proof: one command, one role, one task, streamed to this terminal
// exactly the way `nanobots run` streams a swarm's log.
const teamUsage = `usage: nanobots team run <role> "<task>" [--repo <dir>]
       nanobots team list [--repo <dir>]

  run <role> "<task>"   give that role one task in its own persistent workspace
  list                  roles that have a workspace under ~/.nanobots/team

A role's workspace is a real git worktree of this repo, on its own branch
(team/<role>), kept between tasks rather than thrown away after one. It
needs its own ANTHROPIC_API_KEY in ~/.secrets/nanobots.env — see
docs/team.md for why this can't run through 1Claw/Shroud, the same reason
the foundry's coding agent needs one.`

func runTeam(args []string) error {
	if len(args) == 0 {
		fmt.Println(teamUsage)
		return nil
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "run":
		return runTeamRun(rest)
	case "list":
		return runTeamList(rest)
	case "-h", "--help":
		fmt.Println(teamUsage)
		return nil
	default:
		return fmt.Errorf("unknown team subcommand %q\n%s", sub, teamUsage)
	}
}

func runTeamRun(args []string) error {
	repoRoot := "."
	var positional []string
	for i := 0; i < len(args); i++ {
		if args[i] == "--repo" {
			i++
			if i >= len(args) {
				return fmt.Errorf("--repo requires a directory")
			}
			repoRoot = args[i]
			continue
		}
		positional = append(positional, args[i])
	}
	if len(positional) != 2 {
		return fmt.Errorf("usage: nanobots team run <role> \"<task>\"")
	}
	role, task := positional[0], positional[1]

	apiKey, err := oneclaw.LoadEnvValue("", "ANTHROPIC_API_KEY")
	if err != nil {
		return err
	}
	teamDir, err := team.DefaultTeamDir()
	if err != nil {
		return err
	}

	fmt.Printf("%s: preparing workspace (team/%s)…\n", role, role)
	events := make(chan foundry.Event)
	done := make(chan error, 1)
	go func() {
		done <- team.Run(context.Background(), team.Config{RepoRoot: repoRoot, TeamDir: teamDir, APIKey: apiKey},
			team.TaskInput{Role: role, Task: task}, events)
	}()

	for {
		select {
		case ev := <-events:
			fmt.Printf("[%s] %s\n", ev.Phase, ev.Msg)
		case err := <-done:
			return err
		}
	}
}

func runTeamList(args []string) error {
	teamDir, err := team.DefaultTeamDir()
	if err != nil {
		return err
	}
	roles, err := team.Roles(teamDir)
	if err != nil {
		return err
	}
	if len(roles) == 0 {
		fmt.Println("No Team roles yet. `nanobots team run <role> \"<task>\"` creates one.")
		return nil
	}
	for _, r := range roles {
		fmt.Println(r)
	}
	return nil
}
