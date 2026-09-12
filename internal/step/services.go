package step

import (
	"fmt"
	"sort"

	"github.com/redbotster/nanobots/internal/schema"
)

// ServiceConfigs is every connected-service credential a run might need,
// in one value.
//
// It exists because the same seven fields were being carried separately
// through four layers: resolved in internal/wiring as a struct, unpacked
// into seven positional arguments to BuildDeps, stored as seven fields on
// the Orchestrator, and assigned one by one onto LiveDeps. Adding a
// provider meant editing all four, and BuildDeps had reached seventeen
// parameters — a signature where transposing two strings compiles fine.
//
// The grouping already existed at the top of that chain and was destroyed
// exactly where it would have helped.
type ServiceConfigs struct {
	// VaultID is the shared "nanobots-main" vault every provider's
	// credential lives in. Empty when 1Claw isn't configured.
	VaultID  string
	Google   GoogleConfig
	GitHub   GitHubConfig
	Slack    SlackConfig
	Stripe   StripeConfig
	HubSpot  HubSpotConfig
	X        XConfig
	LinkedIn LinkedInConfig
}

// serviceDispatcher runs one op against one provider's live API. Each
// closes over LiveDeps for the lazily-built, vault-backed client it needs.
type serviceDispatcher func(l *LiveDeps, svc schema.Service, op string, params map[string]any) (any, error)

// serviceDispatchers maps a bot's declared `services[].provider` to the
// code that serves it.
//
// A map rather than the if-chain this replaces. The chain was six
// `if svc.Provider == "..."` branches that each built a client and called a
// dispatch function, all structurally identical — so adding a provider
// meant copying a branch and hoping you changed every name in it, and
// nothing could enumerate what was supported without reading the function.
//
// Registered here rather than by each provider's own file calling an init()
// so the supported set is one readable list, and so its order can't depend
// on file names.
var serviceDispatchers = map[string]serviceDispatcher{
	"google": func(l *LiveDeps, _ schema.Service, op string, params map[string]any) (any, error) {
		c, err := l.googleClient()
		if err != nil {
			return nil, err
		}
		return dispatchGoogle(c, op, params, l.Blobstore)
	},
	"github": func(l *LiveDeps, _ schema.Service, op string, params map[string]any) (any, error) {
		c, err := l.githubClient()
		if err != nil {
			return nil, err
		}
		return dispatchGitHub(c, op, params)
	},
	"stripe": func(l *LiveDeps, _ schema.Service, op string, params map[string]any) (any, error) {
		c, err := l.stripeClient()
		if err != nil {
			return nil, err
		}
		return dispatchStripe(c, op, params)
	},
	"hubspot": func(l *LiveDeps, _ schema.Service, op string, params map[string]any) (any, error) {
		c, err := l.hubspotClient()
		if err != nil {
			return nil, err
		}
		return dispatchHubSpot(c, op, params)
	},
	"x": func(l *LiveDeps, _ schema.Service, op string, params map[string]any) (any, error) {
		c, err := l.xClient()
		if err != nil {
			return nil, err
		}
		return dispatchX(c, op, params)
	},
	"linkedin": func(l *LiveDeps, _ schema.Service, op string, params map[string]any) (any, error) {
		c, err := l.linkedinClient()
		if err != nil {
			return nil, err
		}
		return dispatchLinkedIn(c, op, params)
	},
}

// LiveServiceProviders names every provider with a direct integration, in
// a stable order. Used by tests and by the "what can this build actually
// do" surfaces, so the answer comes from the registry rather than from a
// list someone maintains alongside it.
func LiveServiceProviders() []string {
	out := make([]string, 0, len(serviceDispatchers))
	for id := range serviceDispatchers {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// unsupportedProviderError explains a provider with no direct integration.
//
// Reached only when a bot declares a non-demo connection for one, which is
// a deliberate act — so the message says what to do about it rather than
// just refusing. review-responder's google_business_profile is the live
// example, and the one this build genuinely does not have.
func unsupportedProviderError(svc schema.Service) error {
	return fmt.Errorf("service %q has connection: %s, but this build has no direct %s integration "+
		"(it has: %v) — set connection: demo to run it against fixtures, or see docs/connections.md",
		svc.ID, svc.Connection, svc.Provider, LiveServiceProviders())
}
