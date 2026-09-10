// Command nanobots is the Nanobots CLI: init, add, plan, up, run, save,
// publish, compile. See NANOBOTS-BLUEPRINT.md §6. This build implements
// `plan` and `conform`; the rest are stubbed with a clear "not yet" message
// rather than silently doing nothing.
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/redbotster/nanobots/internal/contract"
	"github.com/redbotster/nanobots/internal/planner"
	"github.com/redbotster/nanobots/internal/schema"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cmd, args := os.Args[1], os.Args[2:]
	var err error
	switch cmd {
	case "plan":
		err = runPlan(args)
	case "conform":
		err = runConform(args)
	case "schema":
		err = runSchema(args)
	case "init", "add", "up", "run", "save", "publish", "compile":
		fmt.Fprintf(os.Stderr, "nanobots %s: not implemented in this build yet\n", cmd)
		os.Exit(1)
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

func usage() {
	fmt.Fprintln(os.Stderr, `usage: nanobots <command> [flags]

commands:
  plan -f <swarm.yaml> [--bots <dir>]     type-check a swarm's snaps and print its run DAG
  conform <bot-dir> [--fixtures <dir>]    run a bot's conformance fixtures against its declared ports
  schema --out <dir>                      regenerate schemas/*.json from the Go types in internal/schema
  init, add, up, run, save, publish, compile   not implemented in this build yet`)
}

func runPlan(args []string) error {
	var swarmPath, botsDir string
	botsDir = "bots"
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-f", "--file":
			i++
			if i >= len(args) {
				return fmt.Errorf("%s requires a value", args[i-1])
			}
			swarmPath = args[i]
		case "--bots":
			i++
			if i >= len(args) {
				return fmt.Errorf("--bots requires a value")
			}
			botsDir = args[i]
		default:
			return fmt.Errorf("unknown flag %q", args[i])
		}
	}
	if swarmPath == "" {
		return fmt.Errorf("-f <swarm.yaml> is required")
	}
	result, err := planner.Plan(swarmPath, botsDir)
	if err != nil {
		return err
	}
	fmt.Print(result.Report())
	if !result.OK() {
		os.Exit(1)
	}
	return nil
}

func runConform(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: nanobots conform <bot-dir> [--fixtures <dir>]")
	}
	botDir := args[0]
	fixturesDir := ""
	for i := 1; i < len(args); i++ {
		if args[i] == "--fixtures" && i+1 < len(args) {
			i++
			fixturesDir = args[i]
		}
	}
	report, err := contract.RunConformance(botDir, fixturesDir)
	if err != nil {
		return err
	}
	fmt.Print(report.String())
	if !report.OK() {
		os.Exit(1)
	}
	return nil
}

func runSchema(args []string) error {
	out := "schemas"
	for i := 0; i < len(args); i++ {
		if args[i] == "--out" && i+1 < len(args) {
			i++
			out = args[i]
		}
	}
	if err := writeSchema(out+"/nanobot.schema.json", schema.NanobotJSONSchema()); err != nil {
		return err
	}
	if err := writeSchema(out+"/nanoswarm.schema.json", schema.NanoswarmJSONSchema()); err != nil {
		return err
	}
	fmt.Printf("wrote %s/nanobot.schema.json and %s/nanoswarm.schema.json\n", out, out)
	return nil
}

func writeSchema(path string, doc map[string]any) error {
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}
