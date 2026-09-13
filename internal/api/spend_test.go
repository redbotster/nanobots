package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

// An install with no 1Claw key is a supported setup, not an error — and the
// answer is "not metered here", never a confident $0.00. A zero that
// actually means "no idea" looks like information, which makes it worse
// than saying nothing.
func TestSpendSaysItCannotSeeDirectProviderSpend(t *testing.T) {
	srv := testServer(t)
	srv.OneClaw = nil

	rec := get(srv, "/api/spend")
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200 — no key is a setup, not a failure", rec.Code)
	}
	var got SpendResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Metered {
		t.Error("claimed to be metering spend with no 1Claw client")
	}
	if got.Known {
		t.Error("claimed to know a figure it cannot see")
	}
}
