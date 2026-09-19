package memory

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/redbotster/nanobots/internal/oneclaw"
)

// oneClawTestServer answers the token exchange every real oneclaw.Client
// call makes first, plus whatever handler the test supplies for the actual
// API path — this package can't reach internal/oneclaw's own unexported
// test helpers, so it builds the same shape directly.
func oneClawTestServer(t *testing.T, path string, handle http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/auth/api-key-token" {
			json.NewEncoder(w).Encode(map[string]any{"access_token": "t", "expires_in": 3600})
			return
		}
		if r.URL.Path == path {
			handle(w, r)
			return
		}
		t.Errorf("unexpected path %s", r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestTheOneClawBackendNowClaimsRecall is the flip side of what this test
// used to assert. A live probe once found /v1/agents/{id}/memory/search
// answering every non-empty query with nothing (docs/1claw-feature-requests.md
// #12), so OneClaw deliberately did not implement Recaller. Re-probed after
// 1Claw shipped a fix — exact and partial-word queries now score and rank
// real matches — so the backend advertises Recaller again, and this test
// pins that decision to a mocked version of the now-working response rather
// than to a live call this suite can't make.
func TestTheOneClawBackendNowClaimsRecall(t *testing.T) {
	var store Store = &OneClaw{Client: oneclaw.NewClient("k"), AgentID: "a"}
	if _, ok := RecallerOf(store); !ok {
		t.Fatal("the 1Claw backend no longer advertises recall — see docs/1claw-feature-requests.md #12 " +
			"and re-probe /v1/agents/{id}/memory/search before reverting this")
	}
}

func TestOneClawRecallJoinsSearchMatchesVerbatim(t *testing.T) {
	srv := oneClawTestServer(t, "/v1/agents/a1/memory/search", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["query"] == "" {
			t.Error("Recall sent an empty query")
		}
		json.NewEncoder(w).Encode(map[string]any{
			"results": []map[string]any{
				{"key": "obs-1", "value": "refunds always get escalated to a human", "score": 1.0},
			},
		})
	})
	store := &OneClaw{Client: oneclaw.NewClient("1ck_test"), AgentID: "a1"}
	store.Client.BaseURL = srv.URL

	got, err := Recall(context.Background(), store, "ns", "refunds")
	if err != nil {
		t.Fatalf("Recall: %v", err)
	}
	if got != "refunds always get escalated to a human" {
		t.Errorf("Recall = %q", got)
	}
}

func TestOneClawRecallEmptyMatchesIsNotAnError(t *testing.T) {
	srv := oneClawTestServer(t, "/v1/agents/a1/memory/search", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"results": []map[string]any{}})
	})
	store := &OneClaw{Client: oneclaw.NewClient("1ck_test"), AgentID: "a1"}
	store.Client.BaseURL = srv.URL

	got, err := Recall(context.Background(), store, "ns", "weather forecast tomorrow")
	if err != nil {
		t.Fatalf("Recall: %v", err)
	}
	if got != "" {
		t.Errorf("Recall = %q, want empty", got)
	}
}
