package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/redbotster/nanobots/internal/oneclaw"
)

// `nanobots deploy 1claw` — run the whole thing on a 1Claw Cloud Runtime.
//
// The point is the five bots that still need Docker. Everything else runs
// in-process now (see internal/runner/inprocess.go), so someone without
// Docker can use most of the catalog locally; a hosted runtime is how they
// get the rest, and how a schedule keeps firing with the laptop shut.
//
// Two honest limits, both reported by the command itself rather than
// discovered afterwards:
//
// There is no `nanobots` runtime template. `GET /v1/runtimes/templates`
// returns nine — python, node, hermes, openclaw, openclaude, opencode,
// claude-code, codex, amp — and they are language runtimes and agent
// frameworks, none of which runs a Go binary. So this deploys from a
// container image instead, which POST /v1/runtimes accepts. Until an image
// is published, --image is required.
//
// It cannot push your local swarms. The image carries the catalog and the
// example swarms it was built with, and 1Claw has no file-transfer API for
// a runtime, so a swarm you wrote here does not travel. Filed in
// docs/1claw-feature-requests.md; the workaround is to build your own image
// from this repo with your swarms in it.

type deployOptions struct {
	Image       string
	Slug        string
	AgentName   string
	Environment string
	Yes         bool
	EnvFilePath string
}

func runDeploy(args []string) error {
	if len(args) == 0 || args[0] != "1claw" {
		fmt.Fprintln(os.Stderr, deployUsage)
		return fmt.Errorf("the only deploy target is 1claw")
	}
	opts := deployOptions{AgentName: "nanobots", Environment: "production"}
	for i := 1; i < len(args); i++ {
		next := func() (string, error) {
			i++
			if i >= len(args) {
				return "", fmt.Errorf("%s requires a value", args[i-1])
			}
			return args[i], nil
		}
		var err error
		switch args[i] {
		case "--image":
			opts.Image, err = next()
		case "--slug":
			opts.Slug, err = next()
		case "--agent":
			opts.AgentName, err = next()
		case "--environment":
			opts.Environment, err = next()
		case "--env":
			opts.EnvFilePath, err = next()
		case "--yes", "-y":
			opts.Yes = true
		default:
			return fmt.Errorf("unknown flag %q", args[i])
		}
		if err != nil {
			return err
		}
	}
	return deployTo1Claw(opts, os.Stdin, os.Stdout)
}

func deployTo1Claw(opts deployOptions, in *os.File, out *os.File) error {
	// Arguments before credentials. A missing --image is a usage error and
	// does not depend on how the machine is configured, so checking the key
	// first made the command answer "no 1Claw key configured" to someone who
	// had also forgotten the one flag it cannot run without — and made
	// TestDeployRefusesWithoutAnImage pass on a laptop with a key in
	// ~/.secrets/nanobots.env while failing in CI, which has none.
	if opts.Image == "" {
		return fmt.Errorf(
			"--image is required: there is no `nanobots` runtime template yet, so this deploys " +
				"from a container image.\nBuild and push one from this repo's Dockerfile, then:\n" +
				"    nanobots deploy 1claw --image ghcr.io/you/nanobots:v1")
	}
	if opts.Slug == "" {
		opts.Slug = "nanobots"
	}

	key, err := oneclaw.LoadAPIKey(opts.EnvFilePath)
	if err != nil {
		return err
	}
	if key == "" {
		return fmt.Errorf("no 1Claw key configured — run `nanobots init` first")
	}
	client := oneclaw.NewClient(key)
	if strings.HasPrefix(key, "ocv_") {
		client = oneclaw.NewAgentClient(key)
	}

	// The agent the runtime runs as. Reusing the composer's rather than
	// making another: agents are plan-capped, and this repo already creates
	// more of them than it should (see docs/1claw-feature-requests.md #4).
	agentID, err := findAgentID(client, opts.AgentName)
	if err != nil {
		return err
	}

	body := map[string]any{
		"name":     opts.AgentName,
		"agent_id": agentID,
		"image":    opts.Image,
		// The WebUI is the reason to host this at all.
		"expose_http": true,
		"http_port":   7474,
		"slug":        opts.Slug,
		// Never public: the callback surface under /internal/steps has no
		// auth beyond a per-run token and was written on the assumption that
		// only this machine could reach it. Exposing that to the internet
		// would be the single worst thing this command could do.
		"inbound_auth": "jwt",
		// Env vars resolve from the vault rather than being passed here, so
		// no credential travels through this process or this repo.
		"environment": opts.Environment,
	}

	fmt.Fprintln(out, "This will create a 1Claw Cloud Runtime, which bills against your account:")
	fmt.Fprintf(out, "\n  image        %s\n", opts.Image)
	fmt.Fprintf(out, "  agent        %s (%s)\n", opts.AgentName, agentID)
	fmt.Fprintf(out, "  url          https://%s.run.1claw.co\n", opts.Slug)
	fmt.Fprintf(out, "  inbound auth jwt (never public — see the comment in deploy.go)\n")
	fmt.Fprintf(out, "  env from     vault environment %q\n", opts.Environment)
	fmt.Fprintln(out, "\nYour locally-written swarms do not travel; the image carries its own.")

	if !opts.Yes {
		fmt.Fprint(out, "\nCreate it? [y/N]: ")
		answer, _ := bufio.NewReader(in).ReadString('\n')
		if strings.ToLower(strings.TrimSpace(answer)) != "y" {
			fmt.Fprintln(out, "Nothing created.")
			return nil
		}
	}

	var created struct {
		ID   string `json:"id"`
		Slug string `json:"slug"`
		URL  string `json:"url"`
	}
	if err := client.PostJSON("/v1/runtimes", body, &created); err != nil {
		return fmt.Errorf("create runtime: %w", err)
	}
	fmt.Fprintf(out, "\nCreated runtime %s.\n", created.ID)

	if err := client.PostJSON("/v1/runtimes/"+created.ID+"/start", map[string]any{}, nil); err != nil {
		fmt.Fprintf(out, "Created but could not start it: %v\nStart it from the 1Claw dashboard.\n", err)
		return nil
	}
	url := created.URL
	if url == "" {
		url = "https://" + opts.Slug + ".run.1claw.co"
	}
	fmt.Fprintf(out, "Started. It will be at %s once it finishes booting.\n", url)
	fmt.Fprintln(out, "Logs: 1claw runtime logs "+created.ID)
	return nil
}

// deployUsage is printed by `nanobots deploy` with no target.
const deployUsage = `usage: nanobots deploy 1claw [flags]

  --image <ref>        container image to run (required; see below)
  --slug <name>        hostname under run.1claw.co (default "nanobots")
  --agent <name>       1Claw agent to run as (default "nanobots")
  --environment <env>  vault environment for env vars (default "production")
  --yes                skip the confirmation

There is no published nanobots image yet and no 1Claw runtime template that
runs a Go binary, so build one from this repo's Dockerfile and push it
somewhere 1Claw can pull from.`

// findAgentID resolves an existing agent by name rather than creating one.
//
// Deliberately not EnsureAgent: agents are plan-capped and this repo
// already makes more of them than it should. A deploy should reuse what is
// there and say so when it is not, not quietly consume another slot.
func findAgentID(client *oneclaw.Client, name string) (string, error) {
	agents, err := client.ListAgents()
	if err != nil {
		return "", fmt.Errorf("list agents: %w", err)
	}
	for _, a := range agents {
		if a.Name == name {
			return a.ID, nil
		}
	}
	return "", fmt.Errorf(
		"no 1Claw agent named %q. Create one first:\n    1claw agent create %s\nor pass --agent with the name of one you have",
		name, name)
}
