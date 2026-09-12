package runner

import (
	"github.com/redbotster/nanobots/internal/memory"
	"github.com/redbotster/nanobots/internal/oneclaw"
	"github.com/redbotster/nanobots/internal/schema"
	"github.com/redbotster/nanobots/internal/step"
)

// BuildDeps resolves the step.Deps a bot instance runs against for one run:
// LiveDeps (real 1Claw Human API + Shroud, with any `connection: demo`
// service still falling back to fixtures) when oc is configured, or pure
// DemoDeps otherwise. Both get the same RunQueueApprover so approvals always
// surface through the run's own queue — see approver.go.
func BuildDeps(run *Run, botID string, nb *schema.Nanobot, oc *oneclaw.Client, agentID, agentAPIKey string, blobs step.BlobStore, google step.GoogleConfig, github step.GitHubConfig, slack step.SlackConfig, stripeCfg step.StripeConfig, hubspot step.HubSpotConfig, xCfg step.XConfig, linkedin step.LinkedInConfig, override step.Approver, mem memory.Store) step.Deps {
	fixturesDir := nb.SourcePath + "/fixtures"
	var approver step.Approver = &RunQueueApprover{Run: run, Bot: botID, Step: "approve"}
	if override != nil {
		approver = override
	}

	if oc == nil || !oc.Configured() {
		d := step.NewDemoDeps(fixturesDir, blobs)
		d.Approver = approver
		return d
	}
	ld := step.NewLiveDeps(oc, oneclaw.NewShroudClient(agentID, agentAPIKey), agentID, fixturesDir, blobs)
	ld.Approver = approver
	ld.Google = google
	ld.GitHub = github
	ld.Slack = slack
	ld.Stripe = stripeCfg
	ld.HubSpot = hubspot
	ld.X = xCfg
	ld.LinkedIn = linkedin
	ld.Memory = mem
	return ld
}
