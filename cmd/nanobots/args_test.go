package main

import (
	"strings"
	"testing"
)

// Every command refuses an argument it does not understand.
//
// `nanobots up` ignored them, and the way that surfaced was the container
// image's entrypoint swallowing "nanobots version" and starting a daemon
// that fired three scheduled swarms. Fixing that one and then checking the
// rest found five more with the same hole: conform, spend, webhook,
// connectors and service all ran happily past a flag they had never heard
// of. None was as dangerous as `up`, and that is not the point — someone
// who types `--dry-run` because they assumed it existed should be told it
// does not, rather than watching the real thing happen.
func TestEveryCommandRefusesAnUnknownFlag(t *testing.T) {
	for _, tc := range []struct {
		name string
		run  func([]string) error
		args []string
	}{
		{"up", runUp, []string{"--nonsense"}},
		{"conform", runConform, []string{"bots", "--nonsense"}},
		{"spend", runSpend, []string{"--nonsense"}},
		{"webhook", runWebhook, []string{"lead-to-meeting", "--nonsense"}},
		{"connectors", runConnectors, []string{"list", "--nonsense"}},
		{"service", runService, []string{"status", "--nonsense"}},
		{"deploy", runDeploy, []string{"1claw", "--nonsense"}},
		{"agents", runAgents, []string{"--nonsense"}},
		{"health", runHealth, []string{"--nonsense"}},
		{"init", runInit, []string{"--nonsense"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.run(tc.args)
			if err == nil {
				t.Fatalf("nanobots %s %v was accepted", tc.name, tc.args)
			}
			if !strings.Contains(err.Error(), "nonsense") {
				t.Errorf("the refusal does not name the argument it refused: %v", err)
			}
		})
	}
}

// And the flags that do exist still work, so the guard did not simply
// refuse everything.
func TestRejectUnknownLetsRealFlagsThrough(t *testing.T) {
	for _, tc := range []struct {
		cmd   string
		args  []string
		known []string
	}{
		{"conform", []string{"bots", "--fixtures", "./x"}, []string{"--fixtures"}},
		{"webhook", []string{"my-swarm", "--addr", "127.0.0.1:1"}, []string{"--addr"}},
		{"agents", []string{"--prune", "--yes"}, []string{"--prune", "--yes"}},
		// A positional argument is the command's own business.
		{"conform", []string{"bots/approve"}, []string{"--fixtures"}},
		{"service", []string{"install"}, nil},
	} {
		if err := rejectUnknown(tc.cmd, tc.args, tc.known...); err != nil {
			t.Errorf("%s %v was refused: %v", tc.cmd, tc.args, err)
		}
	}
}

// The limit of this guard, asserted rather than left to be discovered.
//
// It decides by prefix, so a flag's *value* that starts with a dash looks
// exactly like an unknown flag and is refused. No command in this CLI takes
// such a value — every one is a path, a host:port, a name or a cron
// expression — and the alternative is teaching the guard which flags
// consume a value, which is a second parser to keep in step with the first.
// If a command ever needs `--since -7d`, this is what will have to change.
func TestRejectUnknownRefusesAValueThatLooksLikeAFlag(t *testing.T) {
	err := rejectUnknown("conform", []string{"--fixtures", "-7d"}, "--fixtures")
	if err == nil {
		t.Fatal("a dash-leading value is now accepted — update this test and the comment above it")
	}
	if !strings.Contains(err.Error(), "-7d") {
		t.Errorf("err = %v", err)
	}
}
