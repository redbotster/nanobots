package runner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeLines(t *testing.T, path string, lines ...string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for _, l := range lines {
		if _, err := f.WriteString(l + "\n"); err != nil {
			t.Fatal(err)
		}
	}
}

func msgs(run *Run) []string {
	var out []string
	for _, e := range run.LogEntries() {
		out = append(out, e.Msg)
	}
	return out
}

// The point of the tailer: lines appear while the bot is still running, and
// no line is ever emitted twice however many times we poll.
func TestLogTailEmitsNewLinesOnceEach(t *testing.T) {
	dir := t.TempDir()
	run := NewRun("probe")
	tail := newLogTail(run, "bot", dir)
	path := filepath.Join(dir, "log.jsonl")

	writeLines(t, path, `{"step":"fetch","msg":"one"}`)
	tail.drain()
	tail.drain() // a second poll with nothing new must add nothing
	if got := msgs(run); len(got) != 1 || got[0] != "one" {
		t.Fatalf("after first line: %v", got)
	}

	writeLines(t, path, `{"step":"draft","msg":"two"}`, `{"step":"send","msg":"three"}`)
	tail.drain()
	if got := msgs(run); len(got) != 3 || got[2] != "three" {
		t.Fatalf("after three lines: %v", got)
	}
	tail.drain()
	if got := msgs(run); len(got) != 3 {
		t.Errorf("a redundant drain duplicated lines: %v", got)
	}
}

// The agent flushes a whole line per write, but a reader can still arrive
// mid-write. A half-written line must be skipped and then picked up whole,
// never emitted truncated and never lost.
func TestLogTailSkipsAPartialLineThenPicksItUp(t *testing.T) {
	dir := t.TempDir()
	run := NewRun("probe")
	tail := newLogTail(run, "bot", dir)
	path := filepath.Join(dir, "log.jsonl")

	writeLines(t, path, `{"step":"fetch","msg":"complete"}`)
	// No trailing newline, and cut mid-object.
	f, _ := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o644)
	_, _ = f.WriteString(`{"step":"draft","ms`)
	_ = f.Close()

	tail.drain()
	if got := msgs(run); len(got) != 1 || got[0] != "complete" {
		t.Fatalf("a partial line leaked through: %v", got)
	}

	// The agent finishes the line.
	f, _ = os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o644)
	_, _ = f.WriteString("g\":\"finished\"}\n")
	_ = f.Close()

	tail.drain()
	got := msgs(run)
	if len(got) != 2 || got[1] != "finished" {
		t.Errorf("the completed line was not picked up: %v", got)
	}
}

// A bot that never wrote a log is not a run failure.
func TestLogTailOnAMissingFileIsSilent(t *testing.T) {
	run := NewRun("probe")
	newLogTail(run, "bot", t.TempDir()).drain()
	if got := msgs(run); len(got) != 0 {
		t.Errorf("expected nothing, got %v", got)
	}
}

// The step name reaches the run, since the log column renders "bot/step".
func TestLogTailCarriesTheStepName(t *testing.T) {
	dir := t.TempDir()
	run := NewRun("probe")
	writeLines(t, filepath.Join(dir, "log.jsonl"), `{"step":"brainstorm","msg":"ok"}`)
	newLogTail(run, "ideas", dir).drain()
	e := run.LogEntries()[0]
	if e.Bot != "ideas" || e.Step != "brainstorm" {
		t.Errorf("entry = %+v", e)
	}
}

// A message containing a percent sign must not be re-interpreted as a
// format string on its way onto the run.
func TestLogTailDoesNotFormatTheMessage(t *testing.T) {
	dir := t.TempDir()
	run := NewRun("probe")
	writeLines(t, filepath.Join(dir, "log.jsonl"), `{"step":"s","msg":"100% of 3 items %d"}`)
	newLogTail(run, "bot", dir).drain()
	if got := msgs(run)[0]; !strings.Contains(got, "100% of 3 items %d") {
		t.Errorf("message was reformatted: %q", got)
	}
}
