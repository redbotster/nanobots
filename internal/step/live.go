package step

import (
	"errors"
	"fmt"
	"time"

	"context"

	"github.com/redbotster/nanobots/internal/llm"
	"github.com/redbotster/nanobots/internal/memory"
	"github.com/redbotster/nanobots/internal/oneclaw"
	"github.com/redbotster/nanobots/internal/schema"
)

// LiveDeps runs a bot against real services and a real model, falling back
// to fixture data for any service explicitly marked `connection: demo` —
// which is most of the catalog until someone deliberately connects an
// account (docs/connections.md).
//
// "Real" is now two independent things, and either can be absent. Services
// go to a provider client resolved through serviceDispatchers, or to a
// generic 1Claw execution-intent binding when no direct integration
// exists. ai.generate goes to whatever llm.Generator this deployment
// configured — 1Claw Shroud, or a direct provider key, or nothing, in
// which case it too falls back to fixtures. A machine with only an
// ANTHROPIC_API_KEY gets a LiveDeps with a real model and no live
// services, which is a perfectly good way to run this.
//
// Rendering, the clock, and every demo fallback delegate to an embedded
// DemoDeps rather than duplicating that logic.
type LiveDeps struct {
	OneClaw *oneclaw.Client
	Shroud  *oneclaw.ShroudClient
	AgentID string
	// LLM is where an ai.generate prompt goes — 1Claw Shroud, or a direct
	// provider key. Nil falls back to the bot's fixtures, which is what a
	// deployment with no LLM at all should do rather than failing.
	//
	// Kept separate from Shroud above because those are now different
	// things: Shroud is one backend among several, and this field is the
	// choice between them. See internal/llm.
	LLM llm.Generator
	// Memory is the backend behind memory.* steps — local files, 1Claw, a
	// Honcho server, or a composite of two. See internal/memory.
	Memory    memory.Store
	Blobstore BlobStore
	Demo      *DemoDeps

	// ApprovalPoll/ApprovalTimeout control how Approve and a service call's
	// approval_required retry wait for a human decision. Defaults are set by
	// NewLiveDeps; tests override them to keep polling loops fast.
	ApprovalPoll    time.Duration
	ApprovalTimeout time.Duration

	// Approver, when set, overrides Approve entirely (see Approve below).
	Approver Approver

	// Services holds every connected-service credential in one value —
	// Google, GitHub, Slack, Stripe, HubSpot, X, LinkedIn. A zero field
	// means "not configured", and a non-demo call to that provider fails
	// with a clear error rather than silently falling back to anything.
	//
	// One field rather than seven: see ServiceConfigs for why.
	Services ServiceConfigs

	// Lazily built, vault-backed clients. Private because their lifetime is
	// this LiveDeps' — one run — and nothing outside should hold one.
	googleTS          *googleTokenSource
	githubTokenCache  *vaultToken
	slackTokenCache   *vaultToken
	stripeTokenCache  *vaultToken
	hubspotTokenCache *vaultToken
	xTS               *xTokenSource
	linkedinTS        *linkedinTokenSource
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
	if dispatch, ok := serviceDispatchers[svc.Provider]; ok {
		return dispatch(l, svc, op, params)
	}
	// No direct integration: fall back to a generic 1Claw execution-intent
	// binding, which is how a provider works before anyone writes a client
	// for it. Without 1Claw there is nothing left to try, and saying which
	// providers *are* supported beats a nil-pointer panic.
	if l.OneClaw == nil || !l.OneClaw.Configured() {
		return nil, unsupportedProviderError(svc)
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

// AIGenerate sends the prompt to whichever backend this deployment
// configured. Falling back to fixtures when there is none is deliberate: a
// bot with no LLM behind it produces its demo output and says so in the
// log, rather than failing a whole swarm on a missing key.
func (l *LiveDeps) AIGenerate(prompt string, model schema.Model) (string, error) {
	if l.LLM == nil {
		return l.Demo.AIGenerate(prompt, model)
	}
	return l.LLM.Generate(context.Background(), prompt, model)
}

func (l *LiveDeps) Render(templatePath string, data any, to string) ([]byte, string, error) {
	return l.Demo.Render(templatePath, data, to)
}

func (l *LiveDeps) Now() string { return l.Demo.Now() }

// Memory is where this bot remembers things. Never nil in practice —
// internal/runner always supplies one, defaulting to local files — so
// memory works whether or not 1Claw is configured, which it previously
// did not.
func (l *LiveDeps) memory() memory.Store {
	if l.Memory != nil {
		return l.Memory
	}
	// A LiveDeps built without one still shouldn't panic; behave as a bot
	// with nothing remembered.
	return &memory.Composite{KV: emptyMemory{}}
}

func (l *LiveDeps) MemoryGet(namespace, key string) (string, bool, error) {
	return l.memory().Get(context.Background(), namespace, key)
}

func (l *LiveDeps) MemoryPut(namespace, key, value string) error {
	return l.memory().Put(context.Background(), namespace, key, value)
}

func (l *LiveDeps) MemoryRecall(namespace, question string) (string, error) {
	return memory.Recall(context.Background(), l.memory(), namespace, question)
}

func (l *LiveDeps) MemoryRemember(namespace, text string) error {
	return memory.Remember(context.Background(), l.memory(), namespace, text)
}

// emptyMemory is the "no store configured" fallback: reads find nothing,
// writes are dropped. Only reachable if a caller builds LiveDeps by hand.
type emptyMemory struct{}

func (emptyMemory) Get(context.Context, string, string) (string, bool, error) {
	return "", false, nil
}
func (emptyMemory) Put(context.Context, string, string, string) error { return nil }

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

// WebFetch always does the real, credential-free fetch — there's no
// connection: demo concept for it (it isn't tied to a schema.Service at
// all), so LiveDeps never falls back to DemoDeps here the way ServiceCall
// does for a demo-connection service.
func (l *LiveDeps) WebFetch(params map[string]any) (any, error) {
	return runWebFetch(params)
}

func (l *LiveDeps) Blobs() BlobStore { return l.Blobstore }
