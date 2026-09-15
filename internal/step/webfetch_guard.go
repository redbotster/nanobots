package step

import (
	"context"
	"fmt"
	"net"
	"syscall"
	"time"
)

// web.fetch may not reach this machine, or anything else on its network.
//
// The step takes a URL from a bot's inputs and the fetch runs *in nanobotd*,
// not in the bot's container — so it inherits the daemon's network position,
// which includes loopback. Demonstrated before this existed: a swarm using
// competitor-watch (the one bot declaring `network_egress: ["*"]`) with
//
//	urls: ["http://127.0.0.1:7474/api/swarms/lead-to-meeting/webhook"]
//
// ran to success and put the daemon's own webhook token in its output. From
// there a swarm that notifies or emails has exfiltrated it. The same reach
// covers 169.254.169.254 on a cloud VM and every other service on the host
// or LAN.
//
// EgressPolicy does not help. It is about *which public hosts* a bot
// promised to visit; a bot declaring "*" is declaring it reads arbitrary
// pages from the web, not that it may read the machine it runs on. And an
// empty list means unenforced, which most bots are.
//
// Checked at connect time rather than by inspecting the hostname, because
// a name resolves to whatever its owner says: `localtest.me` already points
// at 127.0.0.1, and a hostile name can answer differently on the second
// lookup (DNS rebinding) so that a check-then-connect passes the check and
// connects somewhere else. Control runs after resolution, on the address
// actually being dialled, and there is no gap between the two.
func guardedDialContext(ctx context.Context, network, address string) (net.Conn, error) {
	return guardedDialer.DialContext(ctx, network, address)
}

var guardedDialer = &net.Dialer{
	Timeout:   10 * time.Second,
	KeepAlive: 30 * time.Second,
	Control: func(network, address string, _ syscall.RawConn) error {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return fmt.Errorf("web.fetch: cannot read the address being dialled (%s)", address)
		}
		ip := net.ParseIP(host)
		if ip == nil {
			return fmt.Errorf("web.fetch: %s is not an IP address", host)
		}
		if allowLoopbackForTest && ip.IsLoopback() {
			return nil
		}
		if reason := blockedAddress(ip); reason != "" {
			return fmt.Errorf("web.fetch: refusing to connect to %s (%s) — a bot fetches pages "+
				"from the web, not services on the machine it runs on", ip, reason)
		}
		return nil
	},
}

// blockedAddress names why an address is off limits, or "" if it is fine.
//
// Named reasons rather than a bare bool: "this resolved to your own
// machine" and "this is a private network address" are different mistakes,
// and a bot author hitting one should be told which.
func blockedAddress(ip net.IP) string {
	switch {
	case ip.IsLoopback():
		return "loopback — this is nanobotd itself, or another service on your machine"
	case ip.IsMulticast(), ip.IsInterfaceLocalMulticast():
		// Before the link-local check: 224.0.0.0/24 is link-local *and*
		// multicast, and calling it cloud metadata would be misleading.
		return "multicast"
	case ip.IsLinkLocalUnicast():
		// 169.254.0.0/16 covers cloud instance metadata, which hands out
		// credentials to anything that asks.
		return "link-local, which is where cloud instance metadata lives"
	case ip.IsPrivate():
		// Covers IPv6 unique-local (fc00::/7) as well as the three IPv4
		// ranges, which is why there is no separate branch for it.
		return "a private network address"
	case ip.IsUnspecified():
		return "unspecified"
	}
	return ""
}

// allowLoopbackForTest lets this package's own tests fetch an httptest
// server, which always binds 127.0.0.1. Unexported and never set outside a
// _test.go file: the guard it bypasses is the one thing standing between a
// bot's input URL and the daemon's own API.
var allowLoopbackForTest bool
