package main

import (
	"bufio"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/redbotster/nanobots/internal/runner"
	"os"
	"path/filepath"
)

// askAndRecord requests one approval on a real Run (so the decision goes
// through the real channel handshake, not a stub) and reports what
// decideApproval decided.
func askAndRecord(t *testing.T, run *runner.Run, stdin *bufio.Reader, nobodyToAsk *bool) (approved bool, decidedBy string) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		approved, decidedBy, _ = run.RequestApproval("email-send-approved", "gate",
			"Re: Contract review", "high", 5*time.Second)
		close(done)
	}()

	deadline := time.After(2 * time.Second)
	for {
		if pending := run.PendingApprovals(); len(pending) > 0 {
			decideApproval(run, pending[0], stdin, nobodyToAsk)
			break
		}
		select {
		case <-deadline:
			t.Fatal("no approval ever became pending")
		case <-time.After(time.Millisecond):
		}
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("RequestApproval never returned")
	}
	return approved, decidedBy
}

// With nothing on stdin — a cron entry, a CI step, anything unattended —
// the run must decline. It already did, but by accident: ReadString
// returned EOF, the empty line didn't start with "y", and the run recorded
// `decided_by=cli` as though a person had sat there and typed no. The
// decision is right; the attribution was a lie, and it's the attribution
// that appears in the failure message someone reads afterwards.
func TestApprovalWithNoTerminalDeclinesAndSaysWhy(t *testing.T) {
	run := runner.NewRun("inbox-autopilot")
	nobodyToAsk := false

	approved, decidedBy := askAndRecord(t, run, bufio.NewReader(strings.NewReader("")), &nobodyToAsk)

	if approved {
		t.Error("an unattended run approved a high-risk send")
	}
	if decidedBy != noTerminalDecider {
		t.Errorf("decided_by = %q, want %q", decidedBy, noTerminalDecider)
	}
	if !nobodyToAsk {
		t.Error("did not remember that there is nobody to ask")
	}
}

// Having learned there's nobody to ask, later approvals must not go back to
// reading stdin — a swarm fanning out over twenty items would otherwise
// print twenty prompts nobody can answer.
func TestOnceThereIsNobodyToAskLaterApprovalsDoNotReadStdin(t *testing.T) {
	run := runner.NewRun("inbox-autopilot")
	nobodyToAsk := true
	stdin := bufio.NewReader(strings.NewReader("y\ny\n"))

	approved, decidedBy := askAndRecord(t, run, stdin, &nobodyToAsk)
	if approved {
		t.Error("approved despite there being nobody to ask")
	}
	if decidedBy != noTerminalDecider {
		t.Errorf("decided_by = %q", decidedBy)
	}
	// The "y" is still there: it was never consumed.
	rest, _ := io.ReadAll(stdin)
	if string(rest) != "y\ny\n" {
		t.Errorf("stdin was consumed anyway: %q left", rest)
	}
}

// A piped answer is a legitimate way to run this — which is why the
// no-terminal case is detected from a read hitting EOF rather than from
// whether stdin is a tty.
func TestPipedAnswersAreHonoured(t *testing.T) {
	for _, tc := range []struct {
		name     string
		input    string
		approved bool
	}{
		{"yes", "y\n", true},
		{"Yes", "Yes\n", true},
		{"no", "n\n", false},
		{"empty line is no", "\n", false},
		{"anything else is no", "maybe\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			run := runner.NewRun("inbox-autopilot")
			nobodyToAsk := false
			approved, decidedBy := askAndRecord(t, run,
				bufio.NewReader(strings.NewReader(tc.input)), &nobodyToAsk)

			if approved != tc.approved {
				t.Errorf("approved = %v, want %v", approved, tc.approved)
			}
			if decidedBy != "cli" {
				t.Errorf("decided_by = %q, want cli — a real answer came from the terminal", decidedBy)
			}
			if nobodyToAsk {
				t.Error("a real answer was mistaken for an empty stdin")
			}
		})
	}
}

// A final answer with no trailing newline still hits EOF, and must be read
// as the answer it is rather than as an empty stdin.
func TestAnAnswerWithoutATrailingNewlineIsStillAnAnswer(t *testing.T) {
	run := runner.NewRun("inbox-autopilot")
	nobodyToAsk := false

	approved, decidedBy := askAndRecord(t, run, bufio.NewReader(strings.NewReader("y")), &nobodyToAsk)

	if !approved {
		t.Error("a typed y without a newline was thrown away")
	}
	if decidedBy != "cli" {
		t.Errorf("decided_by = %q, want cli", decidedBy)
	}
}

// `-f` means the same thing to `plan` and to `run`. It didn't: run joined
// the path onto the repo root unconditionally, so an absolute path came
// back as <repo>/tmp/... — a missing file at a path the user never typed.
func TestAbsoluteSwarmPathsAreLeftAlone(t *testing.T) {
	root := "/Users/someone/nanobots"
	for _, tc := range []struct{ in, want string }{
		{"examples/swarms/inbox-autopilot.yaml", "/Users/someone/nanobots/examples/swarms/inbox-autopilot.yaml"},
		{"./probe.yaml", "/Users/someone/nanobots/probe.yaml"},
		{"/tmp/probe.yaml", "/tmp/probe.yaml"},
		{"/Users/someone/elsewhere/probe.yaml", "/Users/someone/elsewhere/probe.yaml"},
	} {
		if got := resolveSwarmPath(root, tc.in); got != tc.want {
			t.Errorf("resolveSwarmPath(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// Every command in the usage text must be findable by `<cmd> --help`, or
// the central help handler silently falls through to the full usage for a
// command that does exist.
func TestEveryCommandHasItsOwnHelpLine(t *testing.T) {
	var cmds []string
	for _, line := range strings.Split(usageText, "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasSuffix(t, ":") || strings.HasPrefix(t, "usage:") {
			continue
		}
		first := strings.Fields(t)[0]
		// "init, add, save, …" is the not-implemented list.
		if strings.HasSuffix(first, ",") {
			continue
		}
		cmds = append(cmds, first)
	}
	if len(cmds) < 8 {
		t.Fatalf("only found %d commands in the usage text: %v", len(cmds), cmds)
	}
	for _, c := range cmds {
		if !helpFor(c) {
			t.Errorf("`nanobots %s --help` finds nothing", c)
		}
	}
}

func TestWantsHelpSpotsBothForms(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want bool
	}{
		{[]string{"--help"}, true},
		{[]string{"-h"}, true},
		{[]string{"-f", "x.yaml", "--help"}, true},
		{[]string{"-f", "x.yaml"}, false},
		{nil, false},
	} {
		if got := wantsHelp(tc.args); got != tc.want {
			t.Errorf("wantsHelp(%v) = %v, want %v", tc.args, got, tc.want)
		}
	}
}

// `conform bots` used to fail with "open bots/nanobot.yaml: no such file".
// Recognising a directory of bots is what makes the obvious thing work.
func TestBotDirsUnderFindsEveryBotAndNothingElse(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"alpha", "beta"} {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(p, "nanobot.yaml"), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// A directory that is not a bot, and a loose file.
	if err := os.MkdirAll(filepath.Join(dir, "notabot"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	got := botDirsUnder(dir)
	if len(got) != 2 {
		t.Fatalf("found %d bot dirs, want 2: %v", len(got), got)
	}
	for _, g := range got {
		if base := filepath.Base(g); base != "alpha" && base != "beta" {
			t.Errorf("unexpected entry %q", g)
		}
	}
}

func TestBotDirsUnderIsEmptyForAMissingDirectory(t *testing.T) {
	if got := botDirsUnder(filepath.Join(t.TempDir(), "nope")); len(got) != 0 {
		t.Errorf("got %v, want nothing", got)
	}
}

// The summary line for a failing bot has to say what went wrong, not just
// that something did.
func TestFirstProblemPullsTheComplaintOutOfAReport(t *testing.T) {
	report := "conform: probe\n\nconform FAILED:\n  - output port \"count\": expected a string, got float64\n  - and another\n"
	if got := firstProblem(report); got != `output port "count": expected a string, got float64` {
		t.Errorf("firstProblem = %q", got)
	}
	if got := firstProblem("nothing wrong here"); got != "did not conform" {
		t.Errorf("fallback = %q", got)
	}
}
