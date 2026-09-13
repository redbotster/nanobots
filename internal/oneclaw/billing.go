package oneclaw

// What a night of automation cost.
//
// Shroud bills tokens against each agent's budget, and nothing in this
// product ever showed the total. That was tolerable while every run was a
// human clicking Run; it stops being tolerable the moment the scheduler
// keeps nanobotd alive across reboots and fourteen cron triggers fire
// unattended. "This ran thirty times last night" is a question a scheduled
// product has to be able to answer.

// Money here is integer cents, which is how 1Claw reports it and how it
// should stay: a float dollar amount is a rounding error waiting to be
// displayed. Formatting is the caller's problem.

// LLMCreditBalance is prepaid credit, when the account has any.
type LLMCreditBalance struct {
	AvailableCents int64  `json:"available_cents"`
	LedgerCents    int64  `json:"ledger_cents"`
	UsedCents      int64  `json:"used_cents"`
	Currency       string `json:"currency"`
}

// LLMCycleUsage is what has accrued in the current billing period.
type LLMCycleUsage struct {
	// PeriodStart/End bound the cycle, so a figure reads as "since the
	// 1st" rather than as a mystery total.
	PeriodStart       string `json:"period_start"`
	PeriodEnd         string `json:"period_end"`
	AccruedUsageCents int64  `json:"accrued_usage_cents"`
	Currency          string `json:"currency"`
}

// LLMTokenBilling is 1Claw's view of what this account is spending on
// models.
type LLMTokenBilling struct {
	// Enabled is false for an account not on token billing at all, which
	// is a normal state and not an error — the answer is then "1Claw isn't
	// billing your tokens", not a zero that looks like free.
	Enabled            bool              `json:"enabled"`
	SubscriptionStatus string            `json:"subscription_status"`
	CreditBalance      *LLMCreditBalance `json:"credit_balance"`
	CycleUsage         *LLMCycleUsage    `json:"billing_cycle_usage"`
	Warning            string            `json:"warning"`
}

// LLMTokenBillingStatus reports model spend for the whole account.
//
// Account-wide rather than per-run: 1Claw bills per agent and this build
// creates one agent per bot, so the total is what it can honestly report.
// Attributing a figure to a single run would mean inventing arithmetic
// nobody can check.
func (c *Client) LLMTokenBillingStatus() (*LLMTokenBilling, error) {
	var out LLMTokenBilling
	if err := c.do("GET", "/v1/billing/llm-token-billing", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Spent reports what has accrued this cycle, and whether there is a figure
// at all.
//
// Absent is a real state, not zero: an account with token billing on but no
// metered activity yet has no upcoming-invoice line, and rendering that as
// "$0.00 spent" claims a measurement nobody made.
func (b *LLMTokenBilling) Spent() (cents int64, known bool) {
	if b == nil || b.CycleUsage == nil {
		return 0, false
	}
	return b.CycleUsage.AccruedUsageCents, true
}
