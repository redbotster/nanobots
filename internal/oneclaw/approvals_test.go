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

func TestCancelApprovalHitsTheCancelEndpoint(t *testing.T) {
	var gotMethod, gotPath string
	srv := newTestServer(t, map[string]http.HandlerFunc{
		"/v1/auth/api-key-token": tokenHandler(t),
		"/v1/approvals/appr-1/cancel": func(w http.ResponseWriter, r *http.Request) {
			gotMethod, gotPath = r.Method, r.URL.Path
			w.WriteHeader(http.StatusOK)
		},
	})
	c := NewClient("1ck_test")
	c.BaseURL = srv.URL
	if err := c.CancelApproval("appr-1"); err != nil {
		t.Fatalf("CancelApproval: %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/v1/approvals/appr-1/cancel" {
		t.Errorf("request = %s %s, want POST /v1/approvals/appr-1/cancel", gotMethod, gotPath)
	}
}

func TestCancelApprovalReturnsTheServerError(t *testing.T) {
	srv := newTestServer(t, map[string]http.HandlerFunc{
		"/v1/auth/api-key-token": tokenHandler(t),
		"/v1/approvals/appr-1/cancel": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		},
	})
	c := NewClient("1ck_test")
	c.BaseURL = srv.URL
	if err := c.CancelApproval("appr-1"); err == nil {
		t.Error("expected an error for a 404")
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
