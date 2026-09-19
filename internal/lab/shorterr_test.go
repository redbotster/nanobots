package lab

import (
	"fmt"
	"strings"
	"testing"
)

func TestShortErrLeavesAShortErrorAlone(t *testing.T) {
	err := fmt.Errorf("no ANTHROPIC_API_KEY configured")
	if got := shortErr(err); got != err.Error() {
		t.Errorf("shortErr(%q) = %q, want it unchanged", err, got)
	}
}

// Shaped from a real failure: gemini-cli's own rate-limit error prints as
// a multi-hundred-character raw JavaScript exception with a stack trace,
// and the whole thing landed verbatim in Lab's one-line chat summary
// before this existed.
func TestShortErrCapsALongOne(t *testing.T) {
	long := "agent container exited with an error: exit status 1: " + strings.Repeat("at async GeminiChat.streamWithRetries (chunk.js:331605:29)\n", 20)
	got := shortErr(fmt.Errorf("%s", long))
	if len(got) > 350 {
		t.Errorf("shortErr returned %d chars, want it capped near 300", len(got))
	}
	if !strings.HasPrefix(got, long[:50]) {
		t.Errorf("shortErr dropped the start of the message, where the real error usually is: %q", got)
	}
	if !strings.Contains(got, "see the log above") {
		t.Errorf("shortErr doesn't point at where the rest actually is: %q", got)
	}
}
