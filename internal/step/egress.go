package step

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// EgressPolicy enforces a bot's declared guardrails.network_egress.
//
// That list has been in every bot's YAML since the first commit, reported
// in the UI and the docs, and enforced nowhere — "reported, not enforced",
// disclosed honestly but a real hole all the same. A bot could declare
// `network_egress: [googleapis.com]` and fetch anything.
//
// It is enforced here rather than as a container network policy because of
// where the network actually is. Almost nothing a bot does reaches the
// outside from inside its container: service.call, ai.generate, memory,
// approve, notify and web.fetch are all callbacks to nanobotd, which holds
// the credentials. web.fetch is the one step that takes an arbitrary URL
// from a bot's own inputs and goes to it — and it runs on the host, where
// the bot that asked and the URL it asked for are both known. That is the
// hole, and this closes it.
//
// What this does NOT cover, stated plainly rather than implied: a bot using
// the openclaw harness renders HTML in a real Chromium inside its
// container, and remote assets referenced by that HTML are fetched by the
// browser, outside this check. Closing that needs a container-level egress
// proxy — see docs/harnesses.md.
type EgressPolicy struct {
	// Allow is the bot's declared host list. Empty means the bot declared
	// nothing, and nothing is enforced: a promise nobody made is not a
	// promise to break. TestABotThatFetchesDeclaresWhereItMayGo keeps the
	// catalog from using that as a loophole.
	Allow []string
}

// wildcard is a bot declaring it may reach anything. competitor-watch uses
// it: the URLs it fetches come from the user's own input, so no fixed list
// could be right. Honest as an explicit declaration; the point is that it
// has to be one.
const wildcard = "*"

// Check reports whether rawURL is allowed, with an error naming the bot's
// own list — the reader needs to know what the bot promised, not just that
// something was refused.
func (p EgressPolicy) Check(rawURL string) error {
	if len(p.Allow) == 0 {
		return nil
	}
	for _, a := range p.Allow {
		if strings.TrimSpace(a) == wildcard {
			return nil
		}
	}

	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return fmt.Errorf("web.fetch: %q is not a URL: %w", rawURL, err)
	}
	// Anything but http(s) is refused outright rather than matched: file://
	// and gopher:// have no host to check, and a scheme this build has no
	// reason to fetch is not one to be lenient about.
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("web.fetch: %s is not http or https", rawURL)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("web.fetch: %q has no host", rawURL)
	}
	for _, a := range p.Allow {
		if hostMatches(host, strings.TrimSpace(a)) {
			return nil
		}
	}
	return fmt.Errorf("web.fetch: this bot declares it only reaches %s, and %s is not one of them"+
		" — add the host to its guardrails.network_egress, or fetch somewhere it already allows",
		strings.Join(p.Allow, ", "), host)
}

// hostMatches allows an exact host or any subdomain of an allowed one, so
// `googleapis.com` covers `www.googleapis.com` — which is what a reader of
// that list expects it to mean.
//
// The dot is what makes it a suffix match on *labels* rather than on
// characters: without it, "evilgoogleapis.com" would match "googleapis.com"
// and the allowlist would be worse than nothing.
func hostMatches(host, allow string) bool {
	if allow == "" {
		return false
	}
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	allow = strings.ToLower(strings.TrimPrefix(strings.TrimSuffix(allow, "."), "*."))
	if host == allow {
		return true
	}
	// An IP literal is never a subdomain of anything; treating it as one
	// would let "1.2.3.4" match an allowed ".4" style entry.
	if net.ParseIP(host) != nil {
		return false
	}
	return strings.HasSuffix(host, "."+allow)
}
