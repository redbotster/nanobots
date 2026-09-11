package step

import (
	"errors"
	"fmt"
	"time"

	"github.com/redbotster/nanobots/internal/oneclaw"
	"github.com/redbotster/nanobots/internal/schema"
)

// LiveDeps runs a bot against the real 1Claw Human API + Shroud, falling
// back to fixture data for any service explicitly marked
// `connection: demo` in its nanobot.yaml (see bots/*/nanobot.yaml — that's
// exactly the two example bots' Gmail/Drive services right now, pending real
// OAuth wiring). Rendering, the clock, and demo fallback all delegate to an
// embedded DemoDeps rather than duplicating that logic.
type LiveDeps struct {
	OneClaw   *oneclaw.Client
	Shroud    *oneclaw.ShroudClient
	AgentID   string
	Blobstore BlobStore
	Demo      *DemoDeps

	// ApprovalPoll/ApprovalTimeout control how Approve and a service call's
	// approval_required retry wait for a human decision. Defaults are set by
	// NewLiveDeps; tests override them to keep polling loops fast.
	ApprovalPoll    time.Duration
	ApprovalTimeout time.Duration

	// Approver, when set, overrides Approve entirely (see Approve below).
	Approver Approver

	// Google configures direct Gmail/Drive/Sheets access for services with
	// provider: google and a non-demo connection (see google_live.go). Zero
	// value means "not configured" — such a service call fails with a clear
	// error rather than silently falling back to anything.
	Google   GoogleConfig
	googleTS *googleTokenSource

	// GitHub/Slack configure direct access the same way — see
	// github_live.go/slack_live.go. Both are zero-value "not configured" by
	// default too.
	GitHub           GitHubConfig
	githubTokenCache *vaultToken
	Slack            SlackConfig
	slackTokenCache  *vaultToken
}

func NewLiveDeps(oc *oneclaw.Client, shroud *oneclaw.ShroudClient, agentID, fixturesDir string, blobs BlobStore) *LiveDeps {
	return &LiveDeps{
		OneClaw:         oc,
		Shroud:          shroud,
		AgentID:         agentID,
		Blobstore:       blobs,
		Demo:            NewDemoDeps(fixturesDir, blobs),
		ApprovalPoll:    3 * time.Second,
		ApprovalTimeout: 15 * time.Minute,
	}
}

// ServiceCall routes a demo-connection service to fixtures and everything
// else to a real 1Claw execution-intent binding named after the service id.
//
// TODO(nanobots#bindings): binding provisioning (creating the
// gmail/gdrive/etc. binding from a nanobot.yaml services[] entry before this
// ever runs) isn't built yet — this assumes a binding named svc.ID already
// exists on the agent. See internal/runner for where that provisioning step
// belongs once a non-demo service exists to provision.
func (l *LiveDeps) ServiceCall(svc schema.Service, op string, params map[string]any) (any, error) {
	if svc.Connection == schema.ConnectionDemo || svc.Connection == "" {
		return l.Demo.ServiceCall(svc, op, params)
	}
	if svc.Provider == "google" {
		client, err := l.googleClient()
		if err != nil {
			return nil, err
		}
		return dispatchGoogle(client, op, params, l.Blobstore)
	}
	if svc.Provider == "github" {
		client, err := l.githubClient()
		if err != nil {
			return nil, err
		}
		return dispatchGitHub(client, op, params)
	}
	call := func() (*oneclaw.ExecuteResult, error) {
		return l.OneClaw.Execute(l.AgentID, svc.ID, "http", map[string]any{"op": op, "params": params})
	}
	result, err := call()
	var approvalErr *oneclaw.ApprovalRequiredError
	if errors.As(err, &approvalErr) {
		approved, status, werr := l.OneClaw.WaitForApproval(approvalErr.ApprovalID, l.ApprovalPoll, l.ApprovalTimeout)
		if werr != nil {
			return nil, werr
		}
		if !approved {
			return nil, fmt.Errorf("service call %s.%s was not approved (status=%s)", svc.ID, op, status)
		}
		result, err = call()
	}
	if err != nil {
		return nil, err
	}
	return result.Result, nil
}

func (l *LiveDeps) AIGenerate(prompt string, model schema.Model) (string, error) {
	return l.Shroud.Chat(model.Provider, model.Name, prompt, model.MaxTokens)
}

func (l *LiveDeps) Render(templatePath string, data any, to string) ([]byte, string, error) {
	return l.Demo.Render(templatePath, data, to)
}

func (l *LiveDeps) Now() string { return l.Demo.Now() }

func (l *LiveDeps) MemoryGet(namespace, key string) (string, bool, error) {
	return l.OneClaw.MemoryGet(l.AgentID, namespace, key)
}

func (l *LiveDeps) MemoryPut(namespace, key, value string) error {
	return l.OneClaw.MemoryPut(l.AgentID, namespace, key, value)
}

// Approve opens a real 1Claw approval and blocks until a human decides in
// the WebUI, dashboard, or 1Claw mobile app — unless Approver is set, in
// which case that takes over entirely (the local runner uses this to route
// approvals through its own queue; see internal/runner/approver.go).
func (l *LiveDeps) Approve(summary, riskTier string) (bool, string, error) {
	if l.Approver != nil {
		return l.Approver.Approve(summary, riskTier)
	}
	a, err := l.OneClaw.RequestApproval(l.AgentID, summary, riskTier)
	if err != nil {
		return false, "", err
	}
	approved, status, err := l.OneClaw.WaitForApproval(a.ID, l.ApprovalPoll, l.ApprovalTimeout)
	if err != nil {
		return false, "", err
	}
	return approved, "1claw:" + status, nil
}

// Notify delivers for real when channel targets a backend this build knows
// how to reach — today, just Slack ("slack:#channel" or "slack:C0123...").
// Anything else (email:, sms:, or no recognized prefix at all) has no live
// backend wired up yet and falls back to DemoDeps's no-op, a disclosed gap
// rather than a silently faked delivery.
func (l *LiveDeps) Notify(message, channel string) error {
	if target, ok := slackChannelFrom(channel); ok {
		client, err := l.slackClient()
		if err != nil {
			return err
		}
		_, err = client.PostMessage(target, message)
		return err
	}
	return l.Demo.Notify(message, channel)
}

func (l *LiveDeps) Blobs() BlobStore { return l.Blobstore }
