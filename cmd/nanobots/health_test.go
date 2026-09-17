package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The point of this command is the container healthcheck, so the two cases
// that matter are "the daemon answers" and "it does not" — and the second
// one has to be an error, not a cheerful line and exit 0. A healthcheck that
// cannot fail is worse than no healthcheck, because it turns a dead
// container into a healthy one on the dashboard.
func TestHealth(t *testing.T) {
	for _, tc := range []struct {
		name    string
		handler http.HandlerFunc
		wantErr string
	}{
		{
			name: "a daemon answering is not an error",
			handler: func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/status" {
					t.Errorf("health asked for %q, want /api/status", r.URL.Path)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"llm_backend":"1claw shroud","oneclaw_configured":true,"docker_available":true}`))
			},
		},
		{
			name: "a daemon that is up but broken is an error",
			handler: func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "everything is on fire", http.StatusInternalServerError)
			},
			wantErr: "answered 500",
		},
		{
			// Whatever /api/status grows next must not turn into a parse
			// failure here: the question this answers is "is it answering".
			name: "an unreadable body is still an answer",
			handler: func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(`not json at all`))
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(tc.handler)
			defer srv.Close()
			addr := strings.TrimPrefix(srv.URL, "http://")

			err := runHealth([]string{"--addr", addr, "--quiet"})
			switch {
			case tc.wantErr == "" && err != nil:
				t.Fatalf("health = %v, want no error", err)
			case tc.wantErr != "" && err == nil:
				t.Fatalf("health succeeded, want an error mentioning %q", tc.wantErr)
			case tc.wantErr != "" && !strings.Contains(err.Error(), tc.wantErr):
				t.Errorf("health = %v, want it to mention %q", err, tc.wantErr)
			}
		})
	}
}

// Nothing listening is the case the healthcheck exists for.
func TestHealthFailsWhenNothingIsListening(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	addr := strings.TrimPrefix(srv.URL, "http://")
	srv.Close() // the port is now closed, and nothing else has taken it

	err := runHealth([]string{"--addr", addr, "--quiet"})
	if err == nil {
		t.Fatal("health succeeded against a closed port")
	}
	if !strings.Contains(err.Error(), "nanobots up") {
		t.Errorf("the failure does not say what to do about it:\n%v", err)
	}
}

func TestHealthRejectsUnknownFlags(t *testing.T) {
	if err := runHealth([]string{"--json"}); err == nil {
		t.Error("an unknown flag should be refused rather than ignored")
	}
	if err := runHealth([]string{"--addr"}); err == nil {
		t.Error("--addr with no value should be refused")
	}
}
