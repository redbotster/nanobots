// Package secrets is where a real credential lives when a bot needs one.
//
// Until now there was exactly one place: a 1Claw vault, reached through
// internal/oneclaw.Client.GetSecret/PutSecret directly from
// internal/step.vaultToken. That meant a real Slack, GitHub, Stripe or
// HubSpot connection needed a 1Claw account even for someone who only
// wanted the two-minute version — paste a token, run the bot — with
// nowhere else nanobotd would look.
//
// This mirrors internal/llm and internal/memory: one small interface,
// several backends, picked once at startup from NANOBOTS_SECRETS= and
// reported in /api/status, the same shape those two packages already use
// for the identical reason (a pluggable capability that used to be "1Claw
// or nothing").
package secrets

import "errors"

// Store is where a real credential is read from and written to. Every
// backend is keyed by a flat string — "slack/bot_token", "github/token" —
// the same path convention internal/oneclaw's vault already uses, so a
// caller doesn't need to know which backend is behind it.
type Store interface {
	// Get returns a secret's value, or found=false if nothing is stored
	// under key — not an error. A backend that cannot tell "empty" from
	// "not found" (none of the ones here need to) would return found=false
	// for both, which is the same thing a caller does with it either way.
	Get(key string) (value string, found bool, err error)
	Put(key, value string) error
	Delete(key string) error
	// Describe names this backend for logs, /api/status and Settings — and
	// says what protection it actually provides, not just its name, since
	// "1Claw vault" and "a file protected by your OS's own file
	// permissions" are very different promises to make to someone deciding
	// whether to paste a real API key in.
	Describe() string
}

// ErrNotConfigured is returned by a backend that exists but has nothing
// usable yet — a keychain that isn't unlockable in this session, a file
// backend with no key material generated. Named so callers can tell "this
// backend isn't ready" from "the network is down" or "the key was wrong".
var ErrNotConfigured = errors.New("this secrets backend is not configured — see docs/secrets.md")
