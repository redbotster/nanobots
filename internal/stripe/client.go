// Package stripe is a minimal, real Stripe API client for exactly the one
// op this build's bots need: listing invoices. Like Slack/GitHub, a Stripe
// secret key never expires and needs no OAuth dance — paste it in once
// (WebUI Settings, or `nanobots connect stripe`) and it's stored as a 1Claw
// vault secret, never on local disk or inside a bot container.
package stripe

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

var apiBase = "https://api.stripe.com/v1"

type Client struct {
	HTTPClient *http.Client
	Token      string
}

func NewClient(token string) *Client {
	return &Client{HTTPClient: &http.Client{Timeout: 15 * time.Second}, Token: token}
}

// Invoice is the shape bots/invoice-chaser/fixtures/*.json also uses.
// AmountDue is in the currency's smallest unit (cents for USD), matching
// Stripe's own representation rather than converting — a bot's prompt is
// told this explicitly rather than this client silently guessing a currency.
type Invoice struct {
	ID               string `json:"id"`
	CustomerEmail    string `json:"customer_email"`
	AmountDue        int64  `json:"amount_due"`
	DueDate          int64  `json:"due_date,omitempty"`
	HostedInvoiceURL string `json:"hosted_invoice_url"`
	Status           string `json:"status"`
}

type invoicesListResponse struct {
	Data []Invoice `json:"data"`
}

// InvoicesList implements `invoices.list`: up to max invoices matching
// status ("open" for overdue-candidate invoices — Stripe's own status enum,
// not this build's invention).
func (c *Client) InvoicesList(status string, max int) ([]Invoice, error) {
	q := url.Values{}
	if status != "" {
		q.Set("status", status)
	}
	if max > 0 {
		q.Set("limit", fmt.Sprintf("%d", max))
	}
	req, err := http.NewRequest(http.MethodGet, apiBase+"/invoices?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("stripe: invoices.list: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("stripe: invoices.list failed (%d): %s", resp.StatusCode, truncate(raw))
	}
	var out invoicesListResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("stripe: parse invoices.list response: %w", err)
	}
	return out.Data, nil
}

func truncate(b []byte) string {
	const max = 400
	s := string(b)
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}
