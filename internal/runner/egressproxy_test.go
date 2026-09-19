package runner

import (
	"bufio"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// A bot with no declared egress and no network interface at all needs no
// proxy — there's nothing for one to add over what NoNetwork already does.
func TestNoProxyStartsWhenNothingNeedsOne(t *testing.T) {
	p, err := StartEgressProxyIfNeeded(nil, true)
	if err != nil {
		t.Fatalf("StartEgressProxyIfNeeded: %v", err)
	}
	if p != nil {
		t.Fatal("a proxy started for a bot with no network interface")
	}
	p, err = StartEgressProxyIfNeeded(nil, false)
	if err != nil {
		t.Fatalf("StartEgressProxyIfNeeded: %v", err)
	}
	if p != nil {
		t.Fatal("a proxy started for a bot with no declared egress — a promise nobody made is not one to enforce")
	}
}

func TestAProxyStartsWhenEgressIsDeclaredAndNetworked(t *testing.T) {
	p, err := StartEgressProxyIfNeeded([]string{"example.com"}, false)
	if err != nil {
		t.Fatalf("StartEgressProxyIfNeeded: %v", err)
	}
	if p == nil {
		t.Fatal("expected a proxy for a networked bot with declared egress")
	}
	defer p.Close()
	if !strings.HasPrefix(p.Addr(), "host.docker.internal:") {
		t.Errorf("Addr() = %q, want a host.docker.internal address", p.Addr())
	}
}

// dialAddr is the address to actually dial from this test process — the
// real host.docker.internal name only resolves from inside a container,
// so tests reach the same listener by its loopback address directly (the
// unexported ln field, available because this test is in-package).
func dialAddr(p *EgressProxy) string {
	return p.ln.Addr().String()
}

// The plain-HTTP path: an allowed host is proxied through, a disallowed one
// is refused before anything is fetched on the bot's behalf.
func TestProxyRefusesAPlainHTTPRequestToADisallowedHost(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("hello"))
	}))
	defer upstream.Close()

	p, err := StartEgressProxyIfNeeded([]string{"nowhere.example"}, false)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	resp, err := proxyGET(t, dialAddr(p), upstream.URL)
	if err != nil {
		t.Fatalf("GET through proxy: %v", err)
	}
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want 403 for a host not on the allowlist", resp.StatusCode)
	}
}

func TestProxyAllowsAPlainHTTPRequestToAnAllowedHost(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("hello"))
	}))
	defer upstream.Close()
	upstreamHost, _, _ := net.SplitHostPort(upstream.Listener.Addr().String())

	p, err := StartEgressProxyIfNeeded([]string{upstreamHost}, false)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	resp, err := proxyGET(t, dialAddr(p), upstream.URL)
	if err != nil {
		t.Fatalf("GET through proxy: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200 for an allowed host", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "hello" {
		t.Errorf("body = %q", body)
	}
}

// proxyGET routes a real client request through the proxy at proxyAddr, the
// same way Go's http.Transport (and Chrome, with --proxy-server) would.
func proxyGET(t *testing.T, proxyAddr, target string) (*http.Response, error) {
	t.Helper()
	proxyURL, err := url.Parse("http://" + proxyAddr)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)}}
	return client.Get(target)
}

// The CONNECT path is what Chrome actually uses for any https:// resource
// — the tunnel this feature exists for, since TLS keeps everything past
// the CONNECT request opaque to the proxy either way.
func TestProxyRefusesAnHTTPSConnectToADisallowedHost(t *testing.T) {
	p, err := StartEgressProxyIfNeeded([]string{"allowed.example"}, false)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	conn, err := net.DialTimeout("tcp", dialAddr(p), 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	target := "attacker.example:443"
	if _, err := conn.Write([]byte("CONNECT " + target + " HTTP/1.1\r\nHost: " + target + "\r\n\r\n")); err != nil {
		t.Fatal(err)
	}
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("CONNECT status = %d, want 403 for a disallowed host", resp.StatusCode)
	}
}

func TestProxyTunnelsAnHTTPSConnectToAnAllowedHost(t *testing.T) {
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("secure hello"))
	}))
	defer upstream.Close()
	upstreamHost, upstreamPort, _ := net.SplitHostPort(upstream.Listener.Addr().String())

	p, err := StartEgressProxyIfNeeded([]string{upstreamHost}, false)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	// A CONNECT tunnel through the proxy, then a real TLS handshake with
	// the test server's own cert over it — end to end, the same shape
	// Chrome uses for any https:// resource.
	conn, err := net.DialTimeout("tcp", dialAddr(p), 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	target := net.JoinHostPort(upstreamHost, upstreamPort)
	if _, err := conn.Write([]byte("CONNECT " + target + " HTTP/1.1\r\nHost: " + target + "\r\n\r\n")); err != nil {
		t.Fatal(err)
	}
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("CONNECT status = %d, want 200", resp.StatusCode)
	}

	pool := x509.NewCertPool()
	pool.AddCert(upstream.Certificate())
	tlsConn := tls.Client(conn, &tls.Config{RootCAs: pool, ServerName: upstreamHost})
	defer tlsConn.Close()
	if err := tlsConn.Handshake(); err != nil {
		t.Fatalf("TLS handshake over the tunnel: %v", err)
	}
	if _, err := tlsConn.Write([]byte("GET / HTTP/1.1\r\nHost: " + target + "\r\nConnection: close\r\n\r\n")); err != nil {
		t.Fatal(err)
	}
	httpResp, err := http.ReadResponse(bufio.NewReader(tlsConn), nil)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(httpResp.Body)
	if !strings.Contains(string(body), "secure hello") {
		t.Errorf("body over the tunnel = %q", body)
	}
}
