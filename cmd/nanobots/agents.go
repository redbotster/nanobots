package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/redbotster/nanobots/internal/agentname"
	"github.com/redbotster/nanobots/internal/oneclaw"
	"github.com/redbotster/nanobots/internal/runner"
	"github.com/redbotster/nanobots/internal/schema"
)

// `nanobots agents` — what this repo has created on your 1Claw account, and
// what of it is still used.
//
// Agents used to be one per bot name, and are now one per guardrail profile
// (see internal/runner/agentprofile.go). That change does not tidy anything
// up by itself: the old ones keep existing, keep counting against the plan
// cap, and are the reason the cap was in sight to begin with. An account
// that went from 27 agents to 2 in principle is still holding 27.
//
// So this lists them, says which are live under the current scheme, and will
// delete the rest — but only the ones it made, only after printing every
// name, and only on an explicit yes. Deleting someone's agent is not
// something a tool should do quietly on their behalf: an agent's api_key is
// shown exactly once, so a wrong delete is unrecoverable by anyone.

const agentsUsage = `usage: nanobots agents [--prune] [--yes]

  --prune   offer to delete the agents this repo made and no longer uses
  --yes     skip the confirmation (only meaningful with --prune)

With no flags it only lists. Agents this repo did not create are shown and
never touched.`

type agentsOptions struct {
	Prune bool
	Yes   bool
	In    io.Reader
	Out   io.Writer
}

func runAgents(args []string) error {
	opts := agentsOptions{In: os.Stdin, Out: os.Stdout}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--prune":
			opts.Prune = true
		case "--yes", "-y":
			opts.Yes = true
		case "-h", "--help":
			fmt.Println(agentsUsage)
			return nil
		default:
			return fmt.Errorf("unknown flag %q", args[i])
		}
	}
	return listAgents(opts)
}

func listAgents(opts agentsOptions) error {
	key, err := oneclaw.LoadAPIKey("")
	if err != nil {
		return err
	}
	if key == "" {
		return fmt.Errorf("no 1Claw key configured — run `nanobots init` first")
	}
	client := oneclaw.NewClient(key)
	if strings.HasPrefix(key, "ocv_") {
		// Listing agents is control-plane, and an agent key is refused
		// there. Say which key is needed rather than printing 403.
		return fmt.Errorf("listing agents needs a Human key (1ck_); this install has an agent key")
	}

	agents, err := client.ListAgents()
	if err != nil {
		return fmt.Errorf("list agents: %w", err)
	}
	wanted, err := agentsTheCatalogWants()
	if err != nil {
		return err
	}

	live, stale, theirs := classifyAgents(agents, wanted)

	out := opts.Out
	fmt.Fprintf(out, "%d agents on this 1Claw account.\n", len(agents))
	printAgents(out, "in use by this catalog", live)
	printAgents(out, "made by nanobots, no longer used", stale)
	printAgents(out, "not made by nanobots — never touched by this command", theirs)

	if len(stale) == 0 {
		fmt.Fprintln(out, "\nNothing to clean up.")
		return nil
	}
	if !opts.Prune {
		fmt.Fprintf(out, "\n%d unused. `nanobots agents --prune` will offer to delete them.\n", len(stale))
		return nil
	}

	fmt.Fprintf(out, "\nThis permanently deletes %d agents from your 1Claw account.\n", len(stale))
	fmt.Fprintln(out, "An agent's api_key is shown once, so this cannot be undone by anyone.")
	if !opts.Yes {
		fmt.Fprint(out, "\nDelete them? [y/N]: ")
		answer, _ := bufio.NewReader(opts.In).ReadString('\n')
		if strings.ToLower(strings.TrimSpace(answer)) != "y" {
			fmt.Fprintln(out, "Nothing deleted.")
			return nil
		}
	}

	stateDir, _ := oneclaw.DefaultStateDir()
	deleted := 0
	for _, a := range stale {
		if err := client.DeleteAgent(a.ID); err != nil {
			fmt.Fprintf(out, "  %-28s could not delete: %v\n", a.Name, err)
			continue
		}
		// The saved credential is worthless once the agent is gone, and
		// leaving it behind is what makes EnsureAgent report a mismatch
		// later. Removed only after the delete succeeded.
		if stateDir != "" {
			_ = os.Remove(filepath.Join(stateDir, a.Name+".json"))
		}
		deleted++
		fmt.Fprintf(out, "  %-28s deleted\n", a.Name)
	}
	fmt.Fprintf(out, "\n%d deleted, %d left.\n", deleted, len(stale)-deleted)
	return nil
}

// agentsTheCatalogWants is the set of agent names the current catalog would
// create, derived from the bots themselves rather than from a list someone
// maintains — the same call the runner makes, so the two cannot disagree
// about what is in use.
func agentsTheCatalogWants() (map[string]bool, error) {
	root, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	// Every fixed name this repo owns, from the one place that holds them.
	// The first version of this command derived "in use" from the catalog
	// alone and offered to delete nanobots-composer and
	// nanobots-shroud-proxy — both live, both holding an api_key 1Claw
	// shows exactly once. See internal/agentname.
	wanted := map[string]bool{}
	for _, name := range agentname.WellKnown() {
		wanted[name] = true
	}
	err = schema.ForEachBotDir(filepath.Join(root, "bots"), func(_ string, nb *schema.Nanobot) {
		if name := runner.AgentNameFor(nb); name != "" {
			wanted[name] = true
		}
	})
	if err != nil {
		return nil, fmt.Errorf("read bots dir (run this from the repo): %w", err)
	}
	return wanted, nil
}

// classifyAgents splits an account's agents three ways: still used, made by
// this repo and now leftover, and somebody else's.
//
// Pure and separately tested because the middle list is what --prune
// deletes. Getting it wrong is unrecoverable — an agent's api_key is shown
// exactly once — and the first version of it did get it wrong, listing the
// live composer and shroud-proxy agents as leftovers because "in use" was
// derived from the bot catalog alone.
func classifyAgents(agents []oneclaw.Agent, wanted map[string]bool) (live, stale, theirs []oneclaw.Agent) {
	for _, a := range agents {
		switch {
		case wanted[a.Name]:
			live = append(live, a)
		case strings.HasPrefix(a.Name, agentname.Prefix):
			stale = append(stale, a)
		default:
			theirs = append(theirs, a)
		}
	}
	sortAgents(live)
	sortAgents(stale)
	sortAgents(theirs)
	return live, stale, theirs
}

func printAgents(out io.Writer, heading string, agents []oneclaw.Agent) {
	if len(agents) == 0 {
		return
	}
	fmt.Fprintf(out, "\n%s (%d):\n", heading, len(agents))
	for _, a := range agents {
		fmt.Fprintf(out, "  %-28s %s\n", a.Name, a.ID)
	}
}

func sortAgents(agents []oneclaw.Agent) {
	sort.Slice(agents, func(i, j int) bool { return agents[i].Name < agents[j].Name })
}
