package api

import (
	"fmt"
	"net/http"
)

// What the model calls have cost.
//
// `nanobots spend` has been able to answer this and the app has not, which
// gets the audiences backwards: the person clicking Run on an LLM swarm all
// afternoon is the one who should be able to see the number without
// learning a subcommand.
//
// Only meaningful when 1Claw is metering the tokens. On a direct provider
// key — Gemini, an OpenAI-shaped gateway, Anthropic — the spend is between
// the user and that provider and nothing here can see it. Reported as
// "not billing here" rather than as zero, because a confident $0.00 that
// actually means "no idea" is the worse answer.

// SpendResponse is the billing figure, in the shape the Model section
// renders.
type SpendResponse struct {
	// Metered is false when 1Claw isn't billing this account's tokens. The
	// rest of the fields mean nothing when it is false.
	Metered bool `json:"metered"`
	// Known distinguishes "$0.00 so far this period" from "nothing has been
	// metered yet", which are different sentences.
	Known      bool    `json:"known"`
	SpentUSD   float64 `json:"spent_usd"`
	PeriodFrom string  `json:"period_from,omitempty"`
	// CreditUSD/CreditUsedUSD are the prepaid balance, when there is one.
	CreditUSD     float64 `json:"credit_usd,omitempty"`
	CreditUsedUSD float64 `json:"credit_used_usd,omitempty"`
	// Warning is 1Claw's own words — a budget nearly spent, a balance
	// running out. Passed through rather than reworded.
	Warning string `json:"warning,omitempty"`
}

func (s *Server) handleSpend(w http.ResponseWriter, r *http.Request) {
	if s.OneClaw == nil || !s.OneClaw.Configured() {
		// Not an error. An install with no 1Claw key is a supported setup,
		// and the Model section says so without a red box.
		writeJSON(w, http.StatusOK, SpendResponse{})
		return
	}
	b, err := s.OneClaw.LLMTokenBillingStatus()
	if err != nil {
		writeError(w, http.StatusBadGateway, fmt.Errorf("1Claw billing: %w", err))
		return
	}
	if !b.Enabled {
		writeJSON(w, http.StatusOK, SpendResponse{})
		return
	}
	out := SpendResponse{Metered: true, Warning: b.Warning}
	if cents, known := b.Spent(); known {
		out.Known = true
		out.SpentUSD = float64(cents) / 100
		out.PeriodFrom = b.CycleUsage.PeriodStart
	}
	if cb := b.CreditBalance; cb != nil {
		out.CreditUSD = float64(cb.AvailableCents) / 100
		out.CreditUsedUSD = float64(cb.UsedCents) / 100
	}
	writeJSON(w, http.StatusOK, out)
}
