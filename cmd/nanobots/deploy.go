package main

import (
	"bufio"
	"fmt"
	"os"
	"sort"
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
// There was no `nanobots` runtime template for a long time. `GET
// /v1/runtimes/templates` used to return nine — python, node, hermes,
// openclaw, openclaude, opencode, claude-code, codex, amp — language
// runtimes and agent frameworks, none of which runs a Go binary. There is a
// tenth now, `binary` ("run a compiled program: a release asset from
// BINARY_URL, or a startup command after cloning a repo"), which is
// exactly the gap this comment used to describe as unclosed
// (docs/1claw-feature-requests.md #11). --template picks it (or any other
// template CreateRuntimeRequest.template accepts); the default with no
// flags at all is still --image, unchanged.
//
// --image used to be required, because there was no published image and
// guessing one would have failed at pull time. There is one now, built by
// .github/workflows/release.yml on every tag, so that is the default and
// --image overrides it. It stays the default when neither --image nor
// --template is given.
//
// It still cannot push your local swarms with any confidence. `--template
// binary` plus `--binary-url`/`--runtime-env` can point a runtime at a
// release asset or feed it env vars, using the one field name
// (`BINARY_URL`) 1Claw's own template description names and the
// `env_public` field CreateRuntimeRequest's schema confirms accepts
// arbitrary keys — but the binary template's *own* full contract (the
// checksum field's real name, whether the repo-clone path takes your
// swarms with it) has not been tried against a real deploy, and this
// command does not pretend otherwise. The image path — building your own
// image from this repo with your swarms already in it — is still the
// verified way to make a swarm you wrote here travel.

// DefaultImage is what a deploy runs when told nothing else: the image this
// repo publishes on every tag. Pinned to a tag rather than :latest, because
// a runtime that silently changes version under a running schedule is the
// kind of surprise a deploy should not sign you up for.
const DefaultImage = "ghcr.io/redbotster/nanobots:v0.1.0"

type deployOptions struct {
	Image       string
	Template    string
	Slug        string
	AgentName   string
	Environment string
	Yes         bool
	EnvFilePath string
	// RuntimeEnv becomes CreateRuntimeRequest.env_public — public
	// (non-vault) env vars for the runtime process itself, the mechanism a
	// template like `binary` reads its own configuration from. --binary-url
	// sets BINARY_URL here specifically, since that's the one field name
	// 1Claw's own template description already names; --runtime-env is the
	// general escape hatch for whatever else a given template needs.
	RuntimeEnv map[string]string
}

func runDeploy(args []string) error {
	if len(args) == 0 || args[0] != "1claw" {
		fmt.Fprintln(os.Stderr, deployUsage)
		return fmt.Errorf("the only deploy target is 1claw")
	}
	opts := deployOptions{AgentName: "nanobots", Environment: "production"}
	if err := parseDeployFlags(&opts, args[1:]); err != nil {
		return err
	}
	return deployTo1Claw(opts, os.Stdin, os.Stdout)
}

// parseDeployFlags fills opts from the flags following `1claw`. Separate
// from runDeploy so the flag shapes (repeatable --runtime-env, the
// --binary-url alias, the KEY=VALUE split) can be checked without a 1Claw
// credential or a live deploy.
func parseDeployFlags(opts *deployOptions, args []string) error {
	for i := 0; i < len(args); i++ {
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
		case "--template":
			opts.Template, err = next()
		case "--binary-url":
			var v string
			if v, err = next(); err == nil {
				if opts.RuntimeEnv == nil {
					opts.RuntimeEnv = map[string]string{}
				}
				opts.RuntimeEnv["BINARY_URL"] = v
			}
		case "--runtime-env":
			var kv string
			if kv, err = next(); err == nil {
				k, v, ok := strings.Cut(kv, "=")
				if !ok {
					return fmt.Errorf("--runtime-env wants KEY=VALUE, got %q", kv)
				}
				if opts.RuntimeEnv == nil {
					opts.RuntimeEnv = map[string]string{}
				}
				opts.RuntimeEnv[k] = v
			}
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
	return nil
}

func deployTo1Claw(opts deployOptions, in *os.File, out *os.File) error {
	// Arguments before credentials, which is the order that reports the
	// more useful problem first. Checking the key first made this answer
	// "no 1Claw key configured" to someone whose real mistake was a flag —
	// and made its test pass on a laptop with a key in
	// ~/.secrets/nanobots.env while failing in CI, which has none.
	opts = withDeployDefaults(opts)

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
		// The WebUI is the reason to host this at all.
		"expose_http": true,
		"http_port":   7474,
		"slug":        opts.Slug,
		// Never public: the callback surface under /internal/steps has no
		// auth beyond a per-run token and was written on the assumption that
		// only this machine could reach it. Exposing that to the internet
		// would be the single worst thing this command could do.
		"inbound_auth": "jwt",
		// Secrets resolve from the vault rather than being passed here, so
		// no credential travels through this process or this repo. Separate
		// from RuntimeEnv below, which is CreateRuntimeRequest's own
		// env_public — deliberately non-secret, and named that in the
		// schema for exactly this reason.
		"environment": opts.Environment,
	}
	// Exactly one of these, matching withDeployDefaults: a template-based
	// runtime (e.g. --template binary) doesn't pull an image, and sending
	// both would claim two different ways to start the same runtime.
	if opts.Image != "" {
		body["image"] = opts.Image
	}
	if opts.Template != "" {
		body["template"] = opts.Template
	}
	if len(opts.RuntimeEnv) > 0 {
		body["env_public"] = opts.RuntimeEnv
	}

	fmt.Fprintln(out, "This will create a 1Claw Cloud Runtime, which bills against your account:")
	if opts.Template != "" {
		fmt.Fprintf(out, "\n  template     %s\n", opts.Template)
	} else {
		fmt.Fprintf(out, "\n  image        %s\n", opts.Image)
	}
	fmt.Fprintf(out, "  agent        %s (%s)\n", opts.AgentName, agentID)
	fmt.Fprintf(out, "  url          https://%s.run.1claw.co\n", opts.Slug)
	fmt.Fprintf(out, "  inbound auth jwt (never public — see the comment in deploy.go)\n")
	fmt.Fprintf(out, "  env from     vault environment %q\n", opts.Environment)
	for _, k := range sortedKeys(opts.RuntimeEnv) {
		fmt.Fprintf(out, "  runtime env  %s=%s\n", k, opts.RuntimeEnv[k])
	}
	if opts.Template != "" {
		fmt.Fprintln(out, "\nThe binary template's own contract (checksum field, whether a repo clone")
		fmt.Fprintln(out, "carries your swarms) has not been verified against a real deploy — see the")
		fmt.Fprintln(out, "comment atop deploy.go before relying on it.")
	} else {
		fmt.Fprintln(out, "\nYour locally-written swarms do not travel; the image carries its own.")
	}

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

  --image <ref>          container image to run (default "ghcr.io/redbotster/nanobots:v0.1.0")
  --template <name>       1Claw runtime template instead of an image (e.g. "binary")
  --binary-url <url>      sets the BINARY_URL env var a "binary" template reads
  --runtime-env KEY=VALUE  another public env var for the runtime (repeatable)
  --slug <name>           hostname under run.1claw.co (default "nanobots")
  --agent <name>          1Claw agent to run as (default "nanobots")
  --environment <env>     vault environment for secrets (default "production")
  --yes                   skip the confirmation

With no --template, this deploys from a container image — the default is
the one published on every tag; pass --image to run your own build, which
is what you want if your swarms need to travel with it. --template picks a
1Claw runtime template instead (e.g. "binary", which runs a compiled
program rather than pulling an image) — its own full contract has not been
verified against a real deploy; see the comment atop deploy.go.`

// withDeployDefaults fills in what the user did not say. Separate from the
// command so the defaults can be checked without a 1Claw account: they are
// what decides which image a runtime pulls, and that should not be
// something only a live deploy can tell you.
func withDeployDefaults(opts deployOptions) deployOptions {
	// Only when neither is set — --template opts out of the image default
	// entirely, rather than sending both and letting 1Claw decide which
	// startup mechanism wins.
	if opts.Image == "" && opts.Template == "" {
		opts.Image = DefaultImage
	}
	if opts.Slug == "" {
		opts.Slug = "nanobots"
	}
	if opts.AgentName == "" {
		opts.AgentName = "nanobots"
	}
	if opts.Environment == "" {
		opts.Environment = "production"
	}
	return opts
}

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

// sortedKeys is only for the confirmation printout — a map has no order of
// its own, and a deploy prompt that lists the same env vars in a different
// order each run would be a strange thing to have to re-read carefully.
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
