package oneclaw

import (
	"encoding/json"
	"errors"
	"fmt"
)

type executeRequest struct {
	Binding    string         `json:"binding"`
	IntentType string         `json:"intent_type"`
	Params     map[string]any `json:"params"`
	DryRun     bool           `json:"dry_run,omitempty"`
}

// ExecuteResult mirrors the live 200 response from POST /v1/agents/{id}/execute.
type ExecuteResult struct {
	ExecutionID       string         `json:"execution_id"`
	Status            string         `json:"status"`
	Result            map[string]any `json:"result"`
	Error             string         `json:"error"`
	DurationMS        int            `json:"duration_ms"`
	RedactionsApplied int            `json:"redactions_applied"`
	ExecutionSurface  string         `json:"execution_surface"`
}

// ErrApprovalRequired is returned by Execute when 1Claw responds 202 —
// the intent needs a human decision before it runs. ApprovalID names the
// pending approval to resolve via the approvals API.
var ErrApprovalRequired = errors.New("oneclaw: approval required")

type ApprovalRequiredError struct {
	ApprovalID string
	ExpiresAt  string
}

func (e *ApprovalRequiredError) Error() string { return "oneclaw: approval required: " + e.ApprovalID }
func (e *ApprovalRequiredError) Unwrap() error { return ErrApprovalRequired }

// Execute runs one execution-intent binding. intentType is the binding's
// type (e.g. "http"); params are passed through to the underlying call. A
// 202 here means /execute's own "approval_required" outcome (not the
// generic 2xx-is-fine case do() applies elsewhere), so this calls
// rawRequest directly and switches on the status itself.
func (c *Client) Execute(agentID, binding, intentType string, params map[string]any) (*ExecuteResult, error) {
	req := executeRequest{Binding: binding, IntentType: intentType, Params: params}
	status, raw, err := c.rawRequest("POST", "/v1/agents/"+agentID+"/execute", req)
	if err != nil {
		return nil, err
	}
	switch status {
	case 200:
		var resp ExecuteResult
		if err := json.Unmarshal(raw, &resp); err != nil {
			return nil, fmt.Errorf("oneclaw: parse execute response: %w", err)
		}
		return &resp, nil
	case 202:
		var pending struct {
			ApprovalID string `json:"approval_id"`
			ExpiresAt  string `json:"expires_at"`
		}
		_ = json.Unmarshal(raw, &pending)
		return nil, &ApprovalRequiredError{ApprovalID: pending.ApprovalID, ExpiresAt: pending.ExpiresAt}
	default:
		return nil, &apiError{Status: status, Body: raw}
	}
}
