package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func corsFor(t *testing.T, origin, method string) *httptest.ResponseRecorder {
	t.Helper()
	srv := testServer(t)
	r := httptest.NewRequest(method, "/api/swarms", nil)
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, r)
	return rec
}

// The whole point. This used to answer "*" to everyone, so any page the
// user had open could start a run or answer a pending approval — which is
// how a swarm sends mail or pays an invoice.
func TestCORSRefusesAnOriginThatIsNotThisMachine(t *testing.T) {
	for _, origin := range []string{
		"https://evil.example",
		"http://evil.example",
		// Both of these start with something that looks like loopback, and
		// a browser really would send them from an attacker's page.
		"http://127.0.0.1.evil.example",
		"http://localhost.evil.example",
		"null",
		"file://",
	} {
		rec := corsFor(t, origin, http.MethodGet)
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Errorf("origin %q was allowed (%q)", origin, got)
		}
	}
}

func TestCORSAllowsALocallyServedUI(t *testing.T) {
	for _, origin := range []string{
		"http://localhost:5173",
		"http://127.0.0.1:5173",
		"http://localhost:4173",
		"https://localhost:5173",
	} {
		rec := corsFor(t, origin, http.MethodGet)
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != origin {
			t.Errorf("origin %q => %q, want it echoed", origin, got)
		}
	}
}

// The response now depends on the Origin, and these routes carry ETags.
// Without Vary a cache could hand one origin's response to another.
func TestCORSVariesOnOrigin(t *testing.T) {
	rec := corsFor(t, "http://localhost:5173", http.MethodGet)
	if rec.Header().Get("Vary") != "Origin" {
		t.Errorf("Vary = %q, want Origin", rec.Header().Get("Vary"))
	}
	// Also on a refusal, since that answer is origin-dependent too.
	rec = corsFor(t, "https://evil.example", http.MethodGet)
	if rec.Header().Get("Vary") != "Origin" {
		t.Errorf("Vary on a refused origin = %q, want Origin", rec.Header().Get("Vary"))
	}
}

// A preflight from a disallowed origin gets 204 with no CORS headers, which
// the browser reads as "not permitted" and leaks nothing about what exists.
func TestCORSPreflightFromAnAttackerIsNotPermitted(t *testing.T) {
	rec := corsFor(t, "https://evil.example", http.MethodOptions)
	if rec.Code != http.StatusNoContent {
		t.Errorf("code = %d", rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Error("a disallowed preflight was granted")
	}
	if rec.Header().Get("Access-Control-Allow-Methods") != "" {
		t.Error("methods were advertised to a disallowed origin")
	}
}

// curl, the vite proxy and a bot container send no Origin. They are not
// browser cross-origin requests and need no headers — but they must still
// be served.
func TestRequestsWithNoOriginAreServedNormally(t *testing.T) {
	rec := corsFor(t, "", http.MethodGet)
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Error("CORS headers were added to a non-browser request")
	}
}
