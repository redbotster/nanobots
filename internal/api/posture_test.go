package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/redbotster/nanobots/internal/oneclaw"
)

// fake1Claw serves the token exchange plus whatever the test needs.
func fake1Claw(t *testing.T, handlers map[string]string) *oneclaw.Client {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/auth/api-key-token", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"token":"t","expires_in":3600}`)
	})
	for path, body := range handlers {
		b := body
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, b) })
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := oneclaw.NewClient("k")
	c.BaseURL = srv.URL
	return c
}

func posture(t *testing.T, srv *Server) PostureResponse {
	t.Helper()
	rec := get(srv, "/api/posture")
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d: %s", rec.Code, rec.Body.String())
	}
	var out PostureResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// The number worth acting on. Every bot name this repo runs takes an agent
// slot, and running out surfaces as a 403 mid-run.
func TestPostureWarnsBeforeTheAgentCapIsHit(t *testing.T) {
	srv := testServer(t)
	srv.OneClaw = fake1Claw(t, map[string]string{
		"/v1/otel/summary":         `{"posture_score":100,"open_threats":0,"pending_approvals":0,"agent_count":44}`,
		"/v1/billing/subscription": `{"tier":"team","usage":{"agents":{"used":44,"limit":50}}}`,
		"/v1/otel/topology":        `{"nodes":[{"id":"a1","kind":"agent","label":"nanobots-inbox-triage"},{"id":"a2","kind":"agent","label":"someone-elses"}],"edges":[]}`,
	})
	got := posture(t, srv)
	if !got.AgentsNearCap {
		t.Errorf("44 of 50 did not trip the warning: %+v", got)
	}
	if got.Agents != 44 || got.AgentLimit != 50 {
		t.Errorf("agents = %d/%d", got.Agents, got.AgentLimit)
	}
	// Separating "your org has these" from "this app made these" is what
	// turns the number into something you can act on.
	if got.NanobotsAgents != 1 {
		t.Errorf("nanobots_agents = %d, want 1", got.NanobotsAgents)
	}
}

func TestPostureIsQuietWhenThereIsRoom(t *testing.T) {
	srv := testServer(t)
	srv.OneClaw = fake1Claw(t, map[string]string{
		"/v1/otel/summary":         `{"posture_score":100,"agent_count":25}`,
		"/v1/billing/subscription": `{"tier":"team","usage":{"agents":{"used":25,"limit":50}}}`,
		"/v1/otel/topology":        `{"nodes":[],"edges":[]}`,
	})
	if got := posture(t, srv); got.AgentsNearCap {
		t.Errorf("25 of 50 should not warn: %+v", got)
	}
}

// No key is a supported setup, not a failure — and everything else in the
// response is meaningless without one, so the UI must be able to tell.
func TestPostureSaysNothingWithoutA1ClawKey(t *testing.T) {
	srv := testServer(t)
	srv.OneClaw = nil
	if got := posture(t, srv); got.Configured {
		t.Errorf("claimed to be configured with no client: %+v", got)
	}
}

// A posture of zero and a posture we could not read are very different, and
// only one of them is alarming. A settings page must not fail to render
// because a third party is down.
func TestPostureReportsAnUnreachable1ClawWithout500ing(t *testing.T) {
	srv := testServer(t)
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/auth/api-key-token", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"token":"t","expires_in":3600}`)
	})
	mux.HandleFunc("/v1/otel/summary", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	})
	s := httptest.NewServer(mux)
	t.Cleanup(s.Close)
	c := oneclaw.NewClient("k")
	c.BaseURL = s.URL
	srv.OneClaw = c

	got := posture(t, srv) // asserts 200
	if got.Error == "" {
		t.Error("an unreachable 1Claw was reported as a clean posture")
	}
	if got.Score != 0 {
		t.Errorf("score = %d — nothing should be invented when the read failed", got.Score)
	}
}

// The quota and topology calls are extras. Losing one costs a detail, not
// the row.
func TestPostureSurvivesAMissingQuota(t *testing.T) {
	srv := testServer(t)
	srv.OneClaw = fake1Claw(t, map[string]string{
		"/v1/otel/summary": `{"posture_score":92,"open_threats":2,"agent_count":7}`,
	})
	got := posture(t, srv)
	if got.Score != 92 || got.Threats != 2 {
		t.Errorf("the core numbers were lost with the quota: %+v", got)
	}
	if got.AgentLimit != 0 {
		t.Errorf("an agent limit was invented: %d", got.AgentLimit)
	}
}

// Three 1Claw reads, 2.8s in sequence on every call, on the page a user
// opens when something is wrong. They now run concurrently and the result
// is cached, which puts two claims in the comments that ought to be checked.
func TestPostureIsCachedOnSuccessAndNotOnFailure(t *testing.T) {
	t.Run("a good reading is served from cache", func(t *testing.T) {
		var summaryReads atomic.Int64
		mux := http.NewServeMux()
		mux.HandleFunc("/v1/auth/api-key-token", func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"token":"t","expires_in":3600}`)
		})
		mux.HandleFunc("/v1/otel/summary", func(w http.ResponseWriter, r *http.Request) {
			summaryReads.Add(1)
			fmt.Fprint(w, `{"posture_score":91,"open_threats":1,"agent_count":5}`)
		})
		up := httptest.NewServer(mux)
		t.Cleanup(up.Close)
		srv := testServer(t)
		srv.OneClaw = oneclaw.NewClient("k")
		srv.OneClaw.BaseURL = up.URL

		if got := posture(t, srv).Score; got != 91 {
			t.Fatalf("score = %d", got)
		}
		first := summaryReads.Load()
		posture(t, srv)
		posture(t, srv)
		if got := summaryReads.Load(); got != first {
			t.Errorf("1Claw was re-read %d -> %d across repeat calls; the cache is not being used", first, got)
		}
	})

	t.Run("a failure is retried, not remembered", func(t *testing.T) {
		// A blip cached for thirty seconds reads as an outage. The first
		// call fails, the second must actually go and ask again.
		var reads atomic.Int64
		mux := http.NewServeMux()
		mux.HandleFunc("/v1/auth/api-key-token", func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"token":"t","expires_in":3600}`)
		})
		mux.HandleFunc("/v1/otel/summary", func(w http.ResponseWriter, r *http.Request) {
			if reads.Add(1) == 1 {
				w.WriteHeader(http.StatusBadGateway)
				return
			}
			fmt.Fprint(w, `{"posture_score":88,"agent_count":3}`)
		})
		up := httptest.NewServer(mux)
		t.Cleanup(up.Close)
		srv := testServer(t)
		srv.OneClaw = oneclaw.NewClient("k")
		srv.OneClaw.BaseURL = up.URL

		if got := posture(t, srv); got.Error == "" {
			t.Fatalf("expected the first call to report the failure, got %+v", got)
		}
		got := posture(t, srv)
		if got.Error != "" {
			t.Errorf("the failure was cached: %q", got.Error)
		}
		if got.Score != 88 {
			t.Errorf("score = %d, want the recovered reading", got.Score)
		}
	})
}
