package google

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"
)

// LoopbackResult is what a completed (or failed) redirect capture produced.
type LoopbackResult struct {
	Code  string
	State string
	Err   error
}

// LoopbackServer briefly listens on 127.0.0.1 to catch Google's OAuth
// redirect — the standard installed-app pattern, and the only reason a
// desktop client doesn't need a real web server anywhere.
type LoopbackServer struct {
	listener net.Listener
	server   *http.Server
	resultCh chan LoopbackResult
}

// StartLoopback picks an ephemeral port and starts listening immediately;
// RedirectURI() is only valid to read after this returns.
func StartLoopback() (*LoopbackServer, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("google: start loopback listener: %w", err)
	}
	ls := &LoopbackServer{listener: listener, resultCh: make(chan LoopbackResult, 1)}
	mux := http.NewServeMux()
	mux.HandleFunc("/", ls.handleRedirect)
	ls.server = &http.Server{Handler: mux}
	go ls.server.Serve(listener)
	return ls, nil
}

func (ls *LoopbackServer) RedirectURI() string {
	return fmt.Sprintf("http://127.0.0.1:%d/", ls.listener.Addr().(*net.TCPAddr).Port)
}

func (ls *LoopbackServer) handleRedirect(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if errStr := q.Get("error"); errStr != "" {
		ls.resultCh <- LoopbackResult{Err: fmt.Errorf("google denied access: %s", errStr)}
	} else {
		ls.resultCh <- LoopbackResult{Code: q.Get("code"), State: q.Get("state")}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, `<!doctype html><html><body style="font-family:sans-serif;text-align:center;padding-top:80px">
<h2>Nanobots is connected.</h2><p>You can close this tab.</p></body></html>`)
}

// Wait blocks until the redirect arrives or the context is done.
func (ls *LoopbackServer) Wait(ctx context.Context) (LoopbackResult, error) {
	select {
	case res := <-ls.resultCh:
		return res, nil
	case <-ctx.Done():
		return LoopbackResult{}, ctx.Err()
	}
}

func (ls *LoopbackServer) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return ls.server.Shutdown(ctx)
}
