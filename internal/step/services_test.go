package step

import (
	"sort"
	"strings"
	"testing"

	"github.com/redbotster/nanobots/internal/schema"
)

// The registry replaced a six-branch if-chain, so the thing worth pinning
// is that it still covers exactly what the chain did — no provider quietly
// lost on the way, none added that nothing implements.
func TestEveryProviderTheCatalogUsesLiveHasADispatcher(t *testing.T) {
	want := []string{"github", "google", "hubspot", "linkedin", "stripe", "x"}
	got := LiveServiceProviders()
	sort.Strings(got)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("live providers = %v, want %v", got, want)
	}
}

// A provider with no direct integration must fail with something
// actionable. It is reached only when someone deliberately sets a non-demo
// connection, so "no" is not a useful answer on its own — and before the
// registry this path fell through to a nil OneClaw and panicked.
//
// review-responder's google_business_profile is the real example: a gap
// this build genuinely has, called out in docs/connections.md.
func TestAnUnsupportedProviderExplainsItselfInsteadOfPanicking(t *testing.T) {
	l := &LiveDeps{Demo: NewDemoDeps(t.TempDir(), nil)}
	svc := schema.Service{ID: "gbp", Provider: "google_business_profile", Connection: "oauth_native"}

	_, err := l.ServiceCall(svc, "reviews.reply", nil)
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{"gbp", "google_business_profile", "connection: demo", "docs/connections.md"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not mention %q: %v", want, err)
		}
	}
	// It names what this build *can* do, from the registry rather than
	// from a list someone maintains beside it.
	for _, p := range LiveServiceProviders() {
		if !strings.Contains(err.Error(), p) {
			t.Errorf("error does not offer %q as an alternative: %v", p, err)
		}
	}
}

// `connection: demo` — and an unset connection, which means the same —
// must reach fixtures before any provider dispatch is considered. That is
// what keeps the whole catalog runnable offline, and it has to hold for a
// provider that does have a live integration.
func TestADemoConnectionNeverReachesALiveProvider(t *testing.T) {
	for _, conn := range []schema.ConnectionMethod{schema.ConnectionDemo, ""} {
		l := &LiveDeps{Demo: NewDemoDeps(t.TempDir(), nil)}
		svc := schema.Service{ID: "gmail", Provider: "google", Connection: conn}

		// No Google config at all: if this reached the live dispatcher it
		// would complain about credentials. It should complain about a
		// missing fixture instead.
		_, err := l.ServiceCall(svc, "messages.list", nil)
		if err == nil || !strings.Contains(err.Error(), "fixture") {
			t.Errorf("connection %q: err = %v, want a fixture error", conn, err)
		}
	}
}

// ServiceConfigs exists so adding a provider is one field rather than four
// edits across four layers. If it is ever unpacked back into loose
// arguments this stops being true, and the seventeen-parameter BuildDeps
// comes back.
func TestServiceConfigsCarriesEveryProviderInOneValue(t *testing.T) {
	cfg := ServiceConfigs{
		VaultID:  "vault-1",
		Google:   GoogleConfig{ClientID: "g", VaultID: "vault-1"},
		GitHub:   GitHubConfig{VaultID: "vault-1"},
		Slack:    SlackConfig{VaultID: "vault-1"},
		Stripe:   StripeConfig{VaultID: "vault-1"},
		HubSpot:  HubSpotConfig{VaultID: "vault-1"},
		X:        XConfig{ClientID: "x", VaultID: "vault-1"},
		LinkedIn: LinkedInConfig{ClientID: "li", VaultID: "vault-1"},
	}
	l := &LiveDeps{Demo: NewDemoDeps(t.TempDir(), nil), Services: cfg}

	if l.Services.Google.ClientID != "g" || l.Services.LinkedIn.ClientID != "li" {
		t.Error("configs did not survive assignment as one value")
	}
	if l.Services.VaultID != "vault-1" {
		t.Error("the shared vault id is part of the same value")
	}
}
