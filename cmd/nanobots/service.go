// `nanobots service` — keep nanobotd running across reboots.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/redbotster/nanobots/internal/service"
)

// runService manages the background job that keeps nanobotd alive.
//
// Fifteen of the eighteen catalog swarms carry a cron trigger and the
// scheduler that fires them works — but only while nanobotd is running,
// which meant a terminal window someone remembered to leave open. A
// catalog written entirely in the future tense has to survive a reboot.
//
// Every path is resolved and printed rather than assumed: this writes a
// file into someone's LaunchAgents, and the least it can do is say exactly
// what it wrote, how to load it, and how to undo it.
func runService(args []string) error {
	if err := rejectUnknown("service", args); err != nil {
		return err
	}
	action := "status"
	if len(args) > 0 {
		action = args[0]
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	switch action {
	case "status":
		path, ok := service.Installed(home)
		if !ok {
			fmt.Printf("not installed (would be %s)\n", path)
			fmt.Println("run `nanobots service install` to keep nanobotd running across reboots")
			return nil
		}
		load, unload := service.LaunchctlHint(path)
		fmt.Printf("installed: %s\n", path)
		fmt.Printf("  load:   %s\n  unload: %s\n", load, unload)
		return nil

	case "install":
		if !service.Supported() {
			return fmt.Errorf("service install is macOS-only in this build")
		}
		bin, err := os.Executable()
		if err != nil {
			return err
		}
		if bin, err = filepath.EvalSymlinks(bin); err != nil {
			return err
		}
		root, err := os.Getwd()
		if err != nil {
			return err
		}
		// A job pointing at `go run`'s temporary binary would work until
		// the next reboot and then not, in a way nobody would connect back
		// to this command.
		if strings.Contains(bin, os.TempDir()) || strings.Contains(bin, "go-build") {
			return fmt.Errorf("this looks like a `go run` build at %s, which will not exist after a reboot —\n"+
				"build it first (go build -o bin/nanobots ./cmd/nanobots) and run that", bin)
		}

		addr := "127.0.0.1:7474"
		for i := 1; i < len(args); i++ {
			if args[i] == "--addr" && i+1 < len(args) {
				i++
				addr = args[i]
			}
		}
		logDir := filepath.Join(home, ".nanobots", "logs")
		path, err := service.Write(home, service.Config{
			Binary: bin, RepoRoot: root, Addr: addr, LogDir: logDir,
		})
		if err != nil {
			return err
		}
		load, unload := service.LaunchctlHint(path)
		fmt.Printf("wrote %s\n", path)
		fmt.Printf("  runs:    %s up --addr %s\n", bin, addr)
		fmt.Printf("  in:      %s\n", root)
		fmt.Printf("  logs:    %s/nanobotd.log\n", logDir)
		fmt.Println()
		fmt.Printf("start it now:  %s\n", load)
		fmt.Printf("undo:          %s && nanobots service uninstall\n", unload)
		fmt.Println()
		fmt.Println(service.DescribeMissedRuns)
		return nil

	case "uninstall":
		path, ok := service.Installed(home)
		if !ok {
			fmt.Println("not installed, nothing to remove")
			return nil
		}
		_, unload := service.LaunchctlHint(path)
		if _, err := service.Remove(home); err != nil {
			return err
		}
		fmt.Printf("removed %s\n", path)
		fmt.Printf("if it is still running: %s\n", unload)
		return nil
	}
	return fmt.Errorf("usage: nanobots service install|status|uninstall")
}
