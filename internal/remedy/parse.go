package remedy

import (
	"regexp"
	"strings"
)

// Errors arrive wrapped in the whole call chain that produced them:
//
//	container exited 1: nanobot-agent: error: bot notify: step "send":
//	callback /internal/steps/notify: 1Claw vault is locked: Passkey
//	verification required to access vault secrets.
//
// Every layer of that prefix is real and worth keeping in the run detail and
// the log. None of it is what a remedy should be matched against: a rule
// looking for "vault is locked" would work here by luck, and a rule looking
// for something near the front of the string would not work at all.
//
// This mirrors parseRunError in web/src/lib/runError.ts, which does the same
// peeling for display. The web keeps its copy for formatting list rows
// client-side, where shipping a parsed field per run would cost more than it
// saves; this one exists so matching happens on the same text in both.
var noise = []*regexp.Regexp{
	regexp.MustCompile(`^container exited -?\d+:\s*`),
	regexp.MustCompile(`^nanobot-agent:\s*`),
	regexp.MustCompile(`^error:\s*`),
	regexp.MustCompile(`^callback /internal/steps/[a-z_]+:\s*`),
	regexp.MustCompile(`^resolve inputs:\s*`),
}

var (
	botPrefix  = regexp.MustCompile(`^bot ([\w-]+):\s*`)
	stepPrefix = regexp.MustCompile(`^step "([^"]+)":\s*`)
)

// takeOrigin pulls `bot notify: step "send":` off the front.
func takeOrigin(s string) (origin, rest string) {
	rest = s
	if m := botPrefix.FindStringSubmatch(rest); m != nil {
		origin = m[1]
		rest = rest[len(m[0]):]
	}
	if m := stepPrefix.FindStringSubmatch(rest); m != nil {
		if origin != "" {
			origin += "/" + m[1]
		} else {
			origin = m[1]
		}
		rest = rest[len(m[0]):]
	}
	return origin, rest
}

// Message is the reason with the transport wrappers stripped, and Origin is
// the "notify/send" that produced it — "" when the error did not come from a
// specific step.
func Parse(raw string) (origin, message string) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", ""
	}
	// Prefixes nest and repeat — a doubled "container exited 1:" was a real
	// bug — so keep peeling until nothing matches rather than assuming an
	// order or a depth.
	for i := 0; i < 12; i++ {
		before := s
		for _, re := range noise {
			s = re.ReplaceAllString(s, "")
		}
		o, rest := takeOrigin(s)
		if o != "" && origin == "" {
			origin = o
		}
		s = rest
		if s == before {
			break
		}
	}
	// Only ever the first line: a Go error can carry a multi-line body.
	message = strings.TrimSpace(strings.SplitN(s, "\n", 2)[0])
	if message == "" {
		message = strings.TrimSpace(raw)
	}
	return origin, message
}

// Message is Parse's reason, for callers that do not care where it came from.
func Message(raw string) string {
	_, m := Parse(raw)
	return m
}
