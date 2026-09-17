// `nanobots plan` — type-check a swarm, or every swarm, and print the DAG.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/redbotster/nanobots/internal/planner"
)

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
	// No -f means the whole catalog. TestPlanAllExampleSwarms has always
	// done this; asking a person to loop over sixteen files by hand to
	// answer "did my port change break anything" was the gap.
	if swarmPath == "" {
		return planAll(botsDir)
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

// planAll type-checks every swarm in examples/swarms, one line each.
//
// Keeps going past a failure for the same reason conformAll does: the
// question is what broke, and the answer is the whole list.
func planAll(botsDir string) error {
	dir := filepath.Join("examples", "swarms")
	files, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil || len(files) == 0 {
		return fmt.Errorf("no swarms found in %s — pass -f <swarm.yaml> to plan one elsewhere", dir)
	}
	sort.Strings(files)
	failed := 0
	for _, f := range files {
		name := strings.TrimSuffix(filepath.Base(f), ".yaml")
		result, err := planner.Plan(f, botsDir)
		switch {
		case err != nil:
			failed++
			fmt.Printf("  FAIL  %-24s %v\n", name, err)
		case !result.OK():
			failed++
			fmt.Printf("  FAIL  %-24s %s\n", name, firstProblem(result.Report()))
		default:
			fmt.Printf("  ok    %s\n", name)
		}
	}
	fmt.Printf("\n%d swarms, %d failed\n", len(files), failed)
	if failed > 0 {
		os.Exit(1)
	}
	return nil
}
