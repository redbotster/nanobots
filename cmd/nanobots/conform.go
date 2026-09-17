// `nanobots conform` — prove a bot honours its own declared ports,
// without Docker and without a network.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/redbotster/nanobots/internal/contract"
)

// runConform checks one bot, or every bot under a directory.
//
// `conform bots` used to fail with "open bots/nanobot.yaml: no such file",
// which is the tool refusing the most obvious thing to type. The test suite
// has always conformed the whole catalog (TestRunConformanceOnLaunchBots);
// a person who just edited the interpreter or a fixture could only check
// one bot at a time, thirty-nine times.
func runConform(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: nanobots conform <bot-dir>|<dir-of-bots> [--fixtures <dir>]")
	}
	if err := rejectUnknown("conform", args, "--fixtures"); err != nil {
		return err
	}
	botDir := args[0]
	fixturesDir := ""
	for i := 1; i < len(args); i++ {
		if args[i] == "--fixtures" {
			i++
			if i >= len(args) {
				return fmt.Errorf("--fixtures requires a directory")
			}
			fixturesDir = args[i]
		}
	}
	// A directory with no nanobot.yaml of its own, but bot directories
	// inside it, means "all of these".
	if _, err := os.Stat(filepath.Join(botDir, "nanobot.yaml")); err != nil {
		if bots := botDirsUnder(botDir); len(bots) > 0 {
			return conformAll(bots, fixturesDir)
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

// botDirsUnder returns every immediate subdirectory holding a nanobot.yaml.
func botDirsUnder(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		p := filepath.Join(dir, e.Name())
		if _, err := os.Stat(filepath.Join(p, "nanobot.yaml")); err == nil {
			out = append(out, p)
		}
	}
	return out
}

// conformAll runs every bot and prints one line each, then a total.
//
// It keeps going after a failure rather than stopping at the first. The
// question being asked is "what did I break", and the answer is the whole
// list, not the first item on it.
func conformAll(botDirs []string, fixturesDir string) error {
	failed := 0
	for _, dir := range botDirs {
		name := filepath.Base(dir)
		report, err := contract.RunConformance(dir, fixturesDir)
		switch {
		case err != nil:
			failed++
			fmt.Printf("  FAIL  %-24s %v\n", name, err)
		case !report.OK():
			failed++
			fmt.Printf("  FAIL  %-24s %s\n", name, firstProblem(report.String()))
		default:
			fmt.Printf("  ok    %s\n", name)
		}
	}
	fmt.Printf("\n%d bots, %d failed\n", len(botDirs), failed)
	if failed > 0 {
		os.Exit(1)
	}
	return nil
}

// firstProblem pulls the first complaint out of a conformance report, so a
// summary line says what went wrong rather than just that something did.
func firstProblem(report string) string {
	for _, line := range strings.Split(report, "\n") {
		if t := strings.TrimSpace(line); strings.HasPrefix(t, "- ") {
			return strings.TrimPrefix(t, "- ")
		}
	}
	return "did not conform"
}
