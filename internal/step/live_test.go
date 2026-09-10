package step

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/redbotster/nanobots/internal/oneclaw"
	"github.com/redbotster/nanobots/internal/schema"
)

func newLiveDepsAgainst(t *testing.T, mux *http.ServeMux) *LiveDeps {
	t.Helper()
	mux.HandleFunc("/v1/auth/api-key-token", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"access_token": "tok", "token_type": "Bearer", "expires_in": 3600})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	oc := oneclaw.NewClient("1ck_test")
	oc.BaseURL = srv.URL
	blobs, err := NewFSBlobStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ld := NewLiveDeps(oc, oneclaw.NewShroudClient("agent-1", "ocv_x"), "agent-1", t.TempDir(), blobs)
	ld.ApprovalPoll = time.Millisecond
	ld.ApprovalTimeout = time.Second
	return ld
}

func TestLiveDepsMemoryRoundTrip(t *testing.T) {
	stored := map[string]string{}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/agents/agent-1/memory/ns/k", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			var body map[string]string
			json.NewDecoder(r.Body).Decode(&body)
			stored["k"] = body["value"]
			json.NewEncoder(w).Encode(map[string]string{"value": stored["k"]})
			return
		}
		v, ok := stored["k"]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"value": v})
	})
	ld := newLiveDepsAgainst(t, mux)

	if err := ld.MemoryPut("ns", "k", "v1"); err != nil {
		t.Fatalf("MemoryPut: %v", err)
	}
	got, ok, err := ld.MemoryGet("ns", "k")
	if err != nil || !ok || got != "v1" {
		t.Fatalf("MemoryGet = %q, %v, %v", got, ok, err)
	}
}

func TestLiveDepsApproveBlocksUntilApproved(t *testing.T) {
	calls := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/approvals/request", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		json.NewEncoder(w).Encode(oneclaw.Approval{ID: "appr-1", Status: "pending"})
	})
	mux.HandleFunc("/v1/approvals/appr-1/status", func(w http.ResponseWriter, r *http.Request) {
		calls++
		status := "pending"
		if calls >= 2 {
			status = "approved"
		}
		json.NewEncoder(w).Encode(map[string]string{"status": status})
	})
	ld := newLiveDepsAgainst(t, mux)

	approved, decidedBy, err := ld.Approve("send it?", "high")
	if err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if !approved {
		t.Error("expected Approve to return true")
	}
	if decidedBy != "1claw:approved" {
		t.Errorf("decidedBy = %q", decidedBy)
	}
}

func TestLiveDepsServiceCallDemoConnectionUsesFixture(t *testing.T) {
	mux := http.NewServeMux()
	ld := newLiveDepsAgainst(t, mux)
	// Point the embedded demo fixtures dir at the real fixtures used
	// elsewhere in the test suite would require a real bot dir; here we just
	// prove that a demo-connection service never hits the network by giving
	// it a fixtures dir with a known fixture file.
	fixturesDir := t.TempDir()
	ld.Demo.FixturesDir = fixturesDir
	writeJSON(t, fixturesDir+"/svc.op.json", map[string]any{"ok": true})

	svc := schema.Service{ID: "svc", Connection: schema.ConnectionDemo}
	result, err := ld.ServiceCall(svc, "op", nil)
	if err != nil {
		t.Fatalf("ServiceCall: %v", err)
	}
	m, ok := result.(map[string]any)
	if !ok || m["ok"] != true {
		t.Errorf("ServiceCall result = %#v", result)
	}
}

func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
}
