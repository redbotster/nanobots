package oneclaw

import (
	"fmt"
	"time"
)

type requestApprovalRequest struct {
	Action           string         `json:"action"`
	TargetType       string         `json:"target_type"`
	TargetID         string         `json:"target_id"`
	Summary          map[string]any `json:"summary"`
	DeclaredRiskTier int            `json:"declared_risk_tier,omitempty"`
}

// Approval mirrors the subset of the live object Nanobots needs.
type Approval struct {
	ID     string `json:"id"`
	Status string `json:"status"` // pending | approved | rejected | expired
}

// riskTierNumber maps the blueprint's string risk tiers to 1Claw's 1-3
// integer scale. Unrecognized values default to the strictest tier (1) —
// gate more, not less, when a bot's nanobot.yaml has a typo here.
func riskTierNumber(tier string) int {
	switch tier {
	case "low":
		return 1
	case "medium":
		return 2
	case "high":
		return 3
	default:
		return 1
	}
}

// RequestApproval opens a real 1Claw approval, visible in the dashboard and
// mobile app (blueprint §3.3's "approve from the WebUI or 1Claw mobile").
func (c *Client) RequestApproval(agentID, summary, riskTier string) (*Approval, error) {
	var a Approval
	req := requestApprovalRequest{
		Action:           "nanobot.approve",
		TargetType:       "agent",
		TargetID:         agentID,
		Summary:          map[string]any{"text": summary},
		DeclaredRiskTier: riskTierNumber(riskTier),
	}
	if err := c.do("POST", "/v1/approvals/request", req, &a); err != nil {
		return nil, err
	}
	return &a, nil
}

type approvalStatusResponse struct {
	Status string `json:"status"`
}

func (c *Client) ApprovalStatus(id string) (string, error) {
	var resp approvalStatusResponse
	if err := c.do("GET", "/v1/approvals/"+id+"/status", nil, &resp); err != nil {
		return "", err
	}
	return resp.Status, nil
}

// CancelApproval withdraws a pending approval this agent opened.
//
// docs/1claw-feature-requests.md #3: this endpoint didn't exist when
// RunQueueApprover.mirror was written, so a question answered locally left
// its 1Claw mirror to expire on its own thirty minutes later — a stale
// question sitting in a real person's queue for half an hour after it no
// longer meant anything. 1Claw shipped the same resource family as decide
// (POST /v1/approvals/{id}/decide) for this.
func (c *Client) CancelApproval(id string) error {
	return c.do("POST", "/v1/approvals/"+id+"/cancel", nil, nil)
}

// WaitForApproval polls until the approval leaves "pending" or timeout
// elapses. Returns (approved bool, terminalStatus string, error).
func (c *Client) WaitForApproval(id string, poll, timeout time.Duration) (bool, string, error) {
	deadline := time.Now().Add(timeout)
	for {
		status, err := c.ApprovalStatus(id)
		if err != nil {
			return false, "", err
		}
		if status != "pending" {
			return status == "approved", status, nil
		}
		if time.Now().After(deadline) {
			return false, "pending", fmt.Errorf("oneclaw: approval %s still pending after %s", id, timeout)
		}
		time.Sleep(poll)
	}
}
