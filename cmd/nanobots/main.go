// Command nanobots is the CLI: `up` serves the app, `run` and `plan` drive
// a swarm, `init` and `connect` set up accounts, and the rest are in
// usageText below.
//
// This file is the front door only — dispatch, help, version. Each command
// lives in its own file next to this one: plan.go, conform.go, run.go, and
// so on. It was one 1203-line file until the four newest commands arrived
// with their own files and their own tests, and the split is what made the
// difference visible: the commands in here were at 20% coverage, and the
// argument-handling bug that let `docker run <image> nanobots version`
// start a daemon lived in the untested half.
package main

import (
	"fmt"
	"os"
	"runtime"
	"strings"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cmd, args := os.Args[1], os.Args[2:]

	// `nanobots run --help` used to answer "unknown flag" — the tool
	// telling you off for typing the most universal thing there is.
	// Handled centrally so every command gets it, including ones added
	// later that forget to.
	if wantsHelp(args) {
		if helpFor(cmd) {
			return
		}
		usage()
		return
	}

	var err error
	switch cmd {
	case "plan":
		err = runPlan(args)
	case "conform":
		err = runConform(args)
	case "schema":
		err = runSchema(args)
	case "up":
		err = runUp(args)
	case "run":
		err = runRun(args)
	case "init":
		err = runInit(args)
	case "deploy":
		err = runDeploy(args)
	case "connect":
		err = runConnect(args)
	case "service":
		err = runService(args)
	case "connectors":
		err = runConnectors(args)
	case "spend":
		err = runSpend(args)
	case "webhook":
		err = runWebhook(args)
	case "health":
		err = runHealth(args)
	case "agents":
		err = runAgents(args)
	case "export":
		err = runExport(args)
	case "import":
		err = runImport(args)
	case "add", "save", "publish", "compile":
		// Four verbs from the blueprint that this build never grew, listed
		// in `--help` for a long time as "not implemented in this build
		// yet" — a help text arguing with itself, and four rows of the
		// command list spent on nothing.
		//
		// They keep an answer rather than a listing, because someone who
		// types one is asking a real question and the real answer exists.
		fmt.Fprintf(os.Stderr, "nanobots %s: there is no such command. %s\n", cmd, insteadOf[cmd])
		os.Exit(1)
	case "-v", "--version", "version":
		printVersion()
		return
	case "-h", "--help", "help":
		usage()
		return
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", cmd)
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// helpFor prints one command's own usage line. `nanobots run --help`
// answered "unknown flag \"--help\"", which is the tool telling you off for
// typing the most universal thing there is.
func helpFor(cmd string) bool {
	for _, line := range strings.Split(usageText, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), cmd+" ") ||
			strings.TrimSpace(line) == cmd {
			fmt.Println(strings.TrimSpace(line))
			return true
		}
	}
	return false
}

// wantsHelp reports whether -h/--help appears in a command's own arguments.
func wantsHelp(args []string) bool {
	for _, a := range args {
		if a == "-h" || a == "--help" {
			return true
		}
	}
	return false
}

// Version is stamped at build time with
// -ldflags "-X main.Version=$(git describe --tags --always --dirty)".
// "dev" when someone just ran `go build`, which is the honest answer rather
// than a made-up number.
var Version = "dev"

// printVersion also reports the Go toolchain, because "which build is this"
// and "built with what" are the same question in a bug report.
func printVersion() {
	fmt.Printf("nanobots %s (%s %s/%s)\n", Version, runtime.Version(), runtime.GOOS, runtime.GOARCH)
}

const usageText = `usage: nanobots <command> [flags]

commands:
  plan [-f <swarm.yaml>] [--bots <dir>]   type-check a swarm's snaps and print its run DAG; no -f checks every swarm
  conform <bot-dir>|bots [--fixtures <d>] run a bot's conformance fixtures; "conform bots" checks all of them
  schema --out <dir>                      regenerate schemas/*.json from the Go types in internal/schema
  up [--addr host:port]                   start nanobotd (REST+SSE API) in the foreground
  run -f <swarm.yaml> [--bots <dir>]      run a swarm to completion, printing its log; prompts on approvals
  init [--env <file>] [--no-browser]      first-run setup: 1Claw (or not), a model (or not), no text editor
  deploy 1claw [--image <ref>]            run nanobots on a 1Claw Cloud Runtime, reachable at {slug}.run.1claw.co
  connect google                          link a real Gmail/Drive/Sheets account (one-time OAuth in your browser)
  service install|status|uninstall        keep nanobotd running across reboots, so cron triggers actually fire
  connectors list|register|install|status one place to register an OAuth app and wire it to a bot
  spend                                   what this account has spent on models
  webhook <swarm> [--addr host:port]      print where to post to fire a webhook swarm
  health [--addr host:port] [--quiet]     ask a running daemon whether it is answering
  agents [--prune]                        the 1Claw agents this repo made, and which are unused
  export -f <swarm.yaml> [-o <file>]      bundle a swarm to hand to someone else
  import <bundle.yaml>                    add a shared swarm to examples/swarms/
  version                                 print the build and Go toolchain`

func usage() { fmt.Fprintln(os.Stderr, usageText) }

// insteadOf answers the four blueprint verbs this build never grew. Each
// one is a thing you can actually do, by another name — which is the only
// reason removing them from the help is an improvement rather than a
// deletion of information.
var insteadOf = map[string]string{
	"add":     "To put a bot in a swarm, open the swarm in the visual builder, or edit its `bots:` list directly.",
	"save":    "The builder saves a swarm when you press Save; `nanobots export` bundles one to hand to someone else.",
	"publish": "To share a swarm, `nanobots export` it and the other person runs `nanobots import`. See docs/sharing.md.",
	"compile": "Nothing is compiled ahead of time: `nanobots plan` type-checks a swarm and `nanobots run` executes it.",
}
