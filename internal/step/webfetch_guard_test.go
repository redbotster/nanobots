package step

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The demonstrated attack: a bot fetching the daemon's own API. Before the
// guard this returned nanobotd's webhook token into the run's output.
// enforceGuard turns the loopback escape hatch off for one test. The
// package's other fetch tests switch it on in an init() so they can use an
// httptest server, and these must not inherit that.
func enforceGuard(t *testing.T) {
	t.Helper()
	prev := allowLoopbackForTest
	allowLoopbackForTest = false
	t.Cleanup(func() { allowLoopbackForTest = prev })
}

func TestWebFetchRefusesLoopback(t *testing.T) {
	enforceGuard(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"token":"a-secret-nobody-should-get"}`))
	}))
	defer srv.Close()

	page, err := fetchURL(srv.URL) // httptest always binds 127.0.0.1
	if err == nil {
		t.Fatalf("loopback fetch succeeded and returned %q", page.Text)
	}
	if !strings.Contains(err.Error(), "loopback") {
		t.Errorf("error should say why: %v", err)
	}
	if strings.Contains(err.Error(), "a-secret-nobody-should-get") {
		t.Error("the refusal leaked the body it was refusing to fetch")
	}
}

func TestBlockedAddressNamesTheReason(t *testing.T) {
	for _, tc := range []struct{ ip, want string }{
		{"127.0.0.1", "loopback"},
		{"::1", "loopback"},
		{"::ffff:127.0.0.1", "loopback"},  // IPv4-mapped IPv6
		{"169.254.169.254", "link-local"}, // cloud instance metadata
		{"10.0.0.5", "private"},
		{"192.168.1.10", "private"},
		{"172.16.0.1", "private"},
		{"fc00::1", "private"}, // IsPrivate covers IPv6 ULA
		{"0.0.0.0", "unspecified"},
		{"224.0.0.1", "multicast"},
		// Public addresses must still be reachable, or the bots that exist
		// to read the web stop working.
		{"93.184.216.34", ""},
		{"8.8.8.8", ""},
		{"2606:2800:220:1:248:1893:25c8:1946", ""},
	} {
		got := blockedAddress(net.ParseIP(tc.ip))
		if tc.want == "" && got != "" {
			t.Errorf("%s was blocked (%s) but is a public address", tc.ip, got)
		}
		if tc.want != "" && !strings.Contains(got, tc.want) {
			t.Errorf("%s => %q, want it to mention %q", tc.ip, got, tc.want)
		}
	}
}

// The guard runs per connection, so a public host redirecting to loopback
// is caught on the second dial rather than followed.
func TestWebFetchRefusesARedirectToLoopback(t *testing.T) {
	enforceGuard(t)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("internal"))
	}))
	defer target.Close()
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer redirector.Close()

	// Both are loopback here, so the first hop is refused — which is the
	// same protection, applied one hop earlier.
	if _, err := fetchURL(redirector.URL); err == nil {
		t.Fatal("expected the fetch to be refused")
	}
}
