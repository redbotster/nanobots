package main

import (
	"fmt"
	"strings"
)

// A flag parser that drops what it does not understand turns a typo into a
// different program.
//
// `nanobots up` proved it: the container image's entrypoint was
// `nanobots up --addr 0.0.0.0:7474`, so `docker run <image> nanobots
// version` appended "nanobots version" to it — and instead of printing a
// version, the daemon started, bound a port and fired three scheduled
// swarms against a real 1Claw account. Fixing that one and then checking
// the rest found five more commands with the same hole: conform, spend,
// webhook, connectors and service each ran happily past an argument they
// had never heard of.
//
// None of those five is as dangerous as `up` was. That is not the point:
// the person who typed `--dry-run` because they assumed it existed is
// entitled to be told it does not, rather than watching the real thing
// happen.

// rejectUnknown returns an error naming the first argument that is not in
// known, so a command can refuse rather than ignore.
//
// Flags only. A positional argument is a command's own business — `conform`
// takes a bot directory, `webhook` takes a swarm name — and this has no way
// to tell a mistyped one from a real one.
func rejectUnknown(cmd string, args []string, known ...string) error {
	allowed := make(map[string]bool, len(known))
	for _, k := range known {
		allowed[k] = true
	}
	for _, a := range args {
		if !strings.HasPrefix(a, "-") || allowed[a] {
			continue
		}
		// A flag's value is not a flag, so `--fixtures ./x` does not trip
		// on "./x"; only something that looks like a flag and is not one.
		usage := "nanobots " + cmd + " --help"
		if len(known) == 0 {
			return fmt.Errorf("unknown flag %q — %s takes none. See %s", a, cmd, usage)
		}
		return fmt.Errorf("unknown flag %q — %s takes %s. See %s",
			a, cmd, strings.Join(known, ", "), usage)
	}
	return nil
}
