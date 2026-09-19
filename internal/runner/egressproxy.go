package runner

import (
	"context"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/redbotster/nanobots/internal/step"
)

// EgressProxy is a forward proxy that lets a container's own Chromium
// (the openclaw harness's transform.render) reach exactly the hosts a
// bot's guardrails.network_egress declares, and nothing else.
//
// It exists because that's the one thing step.EgressPolicy's own host-side
// check does not cover: service.call, ai.generate, memory, approve, notify
// and web.fetch are all callbacks to nanobotd, checked there, but a bot
// using the openclaw harness renders HTML in a real Chromium inside its
// own container, and a remote asset that HTML references is fetched by the
// browser directly — a summary an LLM wrote from a stranger's email,
// rendered to a PDF, could carry an <img src> an attacker chose, and
// Chrome would fetch it with nothing to stop it. Reusing step.EgressPolicy
// here, rather than a second definition of "allowed", is what keeps
// nanobotd's callbacks and a container's own Chrome unable to disagree
// about what a bot may reach.
//
// One process per bot instance that needs it, started before its
// container runs and stopped after — see StartEgressProxyIfNeeded.
type EgressProxy struct {
	policy step.EgressPolicy
	ln     net.Listener
	// checkFn is policy.Check by default (see the check method). Tests that
	// need to prove the tunnel/dial mechanics — not the allowlist logic,
	// which step.EgressPolicy's own tests already cover — override it,
	// because policy.Check refuses loopback outright regardless of what a
	// bot declares (step.EgressPolicy's doc comment on that), and every
	// destination a Go test can actually reach is loopback by construction.
	checkFn func(rawURL string) error
}

// check is what serveConnect/serveHTTP actually call, so a test override
// takes effect without either of them needing to know one exists.
func (p *EgressProxy) check(rawURL string) error {
	if p.checkFn != nil {
		return p.checkFn(rawURL)
	}
	return p.policy.Check(rawURL)
}

// StartEgressProxyIfNeeded starts a proxy scoped to nb's declared
// network_egress, or returns nil if there's nothing for one to do: a bot
// with no declared egress is unrestricted already (a promise nobody made
// is not a promise to break — the same rule EgressPolicy itself states),
// and a bot with no network interface at all (NoNetwork) has nothing a
// proxy could add.
func StartEgressProxyIfNeeded(egress []string, noNetwork bool) (*EgressProxy, error) {
	if noNetwork || len(egress) == 0 {
		return nil, nil
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	p := &EgressProxy{policy: step.EgressPolicy{Allow: egress}, ln: ln}
	go func() {
		_ = (&http.Server{Handler: p}).Serve(p.ln)
	}()
	return p, nil
}

// Addr is host:port a container reaches this proxy at — via
// host.docker.internal, the same hostname RunContainer already adds so a
// bot's callbacks can reach nanobotd.
func (p *EgressProxy) Addr() string {
	_, port, _ := net.SplitHostPort(p.ln.Addr().String())
	return "host.docker.internal:" + port
}

// Close stops accepting new connections. Called once the container it
// served has exited; in-flight tunnels finish on their own once Chrome's
// own process is gone.
func (p *EgressProxy) Close() error {
	if p == nil {
		return nil
	}
	return p.ln.Close()
}

func (p *EgressProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodConnect {
		p.serveConnect(w, r)
		return
	}
	p.serveHTTP(w, r)
}

// serveConnect handles an HTTPS tunnel request — what Chrome actually
// sends for any https:// resource. The tunnel's target host:port is the
// whole of what's checkable and the whole of what needs checking: once
// established, TLS keeps everything past it opaque to a proxy that isn't
// also a man in the middle, which this deliberately isn't — checking the
// host a bot is about to reach needs no visibility into what it sends it.
func (p *EgressProxy) serveConnect(w http.ResponseWriter, r *http.Request) {
	host := r.URL.Hostname()
	if host == "" {
		host, _, _ = net.SplitHostPort(r.Host)
	}
	if err := p.check("https://" + host + "/"); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	dest, err := net.DialTimeout("tcp", r.Host, 10*time.Second)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer dest.Close()

	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "proxy can't tunnel", http.StatusInternalServerError)
		return
	}
	client, _, err := hj.Hijack()
	if err != nil {
		return
	}
	defer client.Close()
	if _, err := client.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n")); err != nil {
		return
	}

	done := make(chan struct{}, 2)
	go func() { io.Copy(dest, client); done <- struct{}{} }()
	go func() { io.Copy(client, dest); done <- struct{}{} }()
	<-done
}

// serveHTTP handles a plain (unencrypted) proxied request — the same check,
// for the http:// case CONNECT doesn't cover.
func (p *EgressProxy) serveHTTP(w http.ResponseWriter, r *http.Request) {
	if err := p.check(r.URL.String()); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	outReq := r.Clone(context.Background())
	outReq.RequestURI = ""
	resp, err := http.DefaultTransport.RoundTrip(outReq)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	for k, vs := range resp.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}
