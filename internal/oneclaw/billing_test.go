package oneclaw

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func billingServer(t *testing.T, body string) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/auth/api-key-token" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "tok", "token_type": "Bearer", "expires_in": 86400,
			})
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	orig := DefaultBaseURL
	DefaultBaseURL = srv.URL
	t.Cleanup(func() { DefaultBaseURL = orig })
	return NewClient("k")
}

// The exact shape the live API returns — captured from a real response
// rather than from the schema, because the two disagreed: the schema
// implied an `amount`, the API sends `available_cents`.
func TestBillingParsesTheShapeTheAPIActuallySends(t *testing.T) {
	c := billingServer(t, `{
	  "enabled": true,
	  "subscription_status": "active",
	  "credit_balance": {"available_cents": 1234, "ledger_cents": 5000, "used_cents": 3766, "currency": "usd"},
	  "billing_cycle_usage": {"accrued_usage_cents": 742, "currency": "usd",
	    "period_start": "2026-09-01T00:00:00Z", "period_end": "2026-10-01T00:00:00Z"}
	}`)
	got, err := c.LLMTokenBillingStatus()
	if err != nil {
		t.Fatal(err)
	}
	if !got.Enabled || got.CreditBalance.AvailableCents != 1234 || got.CreditBalance.UsedCents != 3766 {
		t.Errorf("credit = %+v", got.CreditBalance)
	}
	cents, known := got.Spent()
	if !known || cents != 742 {
		t.Errorf("Spent() = %d, %v", cents, known)
	}
}

// Absent is a real state, not zero. An account with billing on but no
// metered activity yet has no upcoming-invoice line, and rendering that as
// "$0.00 spent" claims a measurement nobody made — which is exactly the
// response this account returns today.
func TestNoUsageYetIsUnknownRatherThanZero(t *testing.T) {
	c := billingServer(t, `{"enabled": true, "subscription_status": "active",
	  "credit_balance": {"available_cents": 0, "ledger_cents": 0, "used_cents": 0, "currency": "usd"}}`)
	got, err := c.LLMTokenBillingStatus()
	if err != nil {
		t.Fatal(err)
	}
	if _, known := got.Spent(); known {
		t.Error("reported a spend figure with no billing cycle in the response")
	}
}

// An account not on token billing at all is normal, and the honest answer
// is "1Claw isn't billing your tokens" rather than a zero that reads free.
func TestBillingDisabledIsNotAnError(t *testing.T) {
	c := billingServer(t, `{"enabled": false}`)
	got, err := c.LLMTokenBillingStatus()
	if err != nil {
		t.Fatal(err)
	}
	if got.Enabled {
		t.Error("enabled")
	}
	if _, known := got.Spent(); known {
		t.Error("reported spend for an account that isn't billed")
	}
}

// A nil status must not panic a caller that is just rendering a page.
func TestSpentOnNilIsSafe(t *testing.T) {
	var b *LLMTokenBilling
	if _, known := b.Spent(); known {
		t.Error("nil reported a figure")
	}
}
