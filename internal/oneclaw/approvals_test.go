package oneclaw

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

func TestRequestApprovalMapsRiskTier(t *testing.T) {
	var gotTier int
	srv := newTestServer(t, map[string]http.HandlerFunc{
		"/v1/auth/api-key-token": tokenHandler(t),
		"/v1/approvals/request": func(w http.ResponseWriter, r *http.Request) {
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			gotTier = int(body["declared_risk_tier"].(float64))
			w.WriteHeader(http.StatusAccepted)
			json.NewEncoder(w).Encode(Approval{ID: "appr-1", Status: "pending"})
		},
	})
	c := NewClient("1ck_test")
	c.BaseURL = srv.URL
	a, err := c.RequestApproval("agent-1", "send it?", "high")
	if err != nil {
		t.Fatalf("RequestApproval: %v", err)
	}
	if a.ID != "appr-1" {
		t.Errorf("approval id = %q, want appr-1", a.ID)
	}
	if gotTier != 3 {
		t.Errorf("declared_risk_tier = %d, want 3 for high", gotTier)
	}
}

func TestWaitForApprovalPollsUntilDecided(t *testing.T) {
	calls := 0
	srv := newTestServer(t, map[string]http.HandlerFunc{
		"/v1/auth/api-key-token": tokenHandler(t),
		"/v1/approvals/appr-1/status": func(w http.ResponseWriter, r *http.Request) {
			calls++
			status := "pending"
			if calls >= 3 {
				status = "approved"
			}
			json.NewEncoder(w).Encode(map[string]string{"status": status})
		},
	})
	c := NewClient("1ck_test")
	c.BaseURL = srv.URL
	approved, status, err := c.WaitForApproval("appr-1", 1*time.Millisecond, time.Second)
	if err != nil {
		t.Fatalf("WaitForApproval: %v", err)
	}
	if !approved || status != "approved" {
		t.Errorf("WaitForApproval = %v, %q, want true, approved", approved, status)
	}
	if calls < 3 {
		t.Errorf("expected at least 3 polls, got %d", calls)
	}
}

func TestWaitForApprovalTimesOut(t *testing.T) {
	srv := newTestServer(t, map[string]http.HandlerFunc{
		"/v1/auth/api-key-token": tokenHandler(t),
		"/v1/approvals/appr-1/status": func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(map[string]string{"status": "pending"})
		},
	})
	c := NewClient("1ck_test")
	c.BaseURL = srv.URL
	_, _, err := c.WaitForApproval("appr-1", 1*time.Millisecond, 10*time.Millisecond)
	if err == nil {
		t.Fatal("expected a timeout error")
	}
}
