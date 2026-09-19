package step

import (
	"strings"
	"testing"
)

// The allowlist has been in every bot's YAML since the first commit,
// reported in the UI and the docs, and enforced nowhere. A bot could
// declare `network_egress: [googleapis.com]` and fetch anything.
func TestADeclaredAllowlistIsEnforced(t *testing.T) {
	p := EgressPolicy{Allow: []string{"googleapis.com", "api.stripe.com"}}

	for _, ok := range []string{
		"https://googleapis.com/x",
		"https://www.googleapis.com/gmail/v1/users/me/messages",
		"http://api.stripe.com/v1/invoices",
		"https://GOOGLEAPIS.COM/x",      // case
		"https://www.googleapis.com./x", // trailing dot
	} {
		if err := p.Check(ok); err != nil {
			t.Errorf("%s was refused: %v", ok, err)
		}
	}
	for _, bad := range []string{
		"https://evil.example/x",
		"https://api.stripe.com.evil.example/x",
	} {
		if err := p.Check(bad); err == nil {
			t.Errorf("%s was allowed", bad)
		}
	}
}

// The dot is what makes it a suffix match on labels rather than on
// characters. Without it "evilgoogleapis.com" matches "googleapis.com" and
// the allowlist is worse than nothing — it reads as protection and isn't.
func TestASuffixIsNotEnoughToMatch(t *testing.T) {
	p := EgressPolicy{Allow: []string{"googleapis.com"}}
	for _, bad := range []string{
		"https://evilgoogleapis.com/x",
		"https://notgoogleapis.com/x",
	} {
		if err := p.Check(bad); err == nil {
			t.Errorf("%s matched on characters rather than labels", bad)
		}
	}
}

// A scheme this build has no reason to fetch is not one to be lenient
// about: file:// and friends have no host to check at all.
func TestOnlyHTTPAndHTTPSAreFetchable(t *testing.T) {
	p := EgressPolicy{Allow: []string{"example.com"}}
	for _, bad := range []string{
		"file:///etc/passwd",
		"gopher://example.com/",
		"ftp://example.com/x",
		"https://",
	} {
		if err := p.Check(bad); err == nil {
			t.Errorf("%s was allowed", bad)
		}
	}
}

// An IP literal is never a subdomain of anything.
func TestAnIPIsNotASubdomain(t *testing.T) {
	p := EgressPolicy{Allow: []string{"4", "example.com"}}
	if err := p.Check("http://1.2.3.4/x"); err == nil {
		t.Error("an IP matched a label-suffix rule")
	}
	// Allowed explicitly, it works — this is about matching, not a ban.
	if err := (EgressPolicy{Allow: []string{"1.2.3.4"}}).Check("http://1.2.3.4/x"); err != nil {
		t.Errorf("an explicitly allowed IP was refused: %v", err)
	}
}

// A bot that declared nothing has promised nothing, and a promise nobody
// made is not one to break. The catalog is held to a higher bar by a
// separate test; third-party bots are not silently broken by this landing.
func TestNoDeclarationEnforcesNothing(t *testing.T) {
	if err := (EgressPolicy{}).Check("https://anywhere.example/x"); err != nil {
		t.Errorf("an undeclared bot was restricted: %v", err)
	}
}

// "*" is a bot saying out loud that it may reach anything —
// competitor-watch fetches URLs the user supplies, so no fixed list could
// be right. The point is that it has to be declared.
func TestAWildcardIsAnExplicitDeclaration(t *testing.T) {
	if err := (EgressPolicy{Allow: []string{"*"}}).Check("https://anywhere.example/x"); err != nil {
		t.Errorf("wildcard refused: %v", err)
	}
}

// Loopback, link-local (the cloud metadata address lives here) and private
// ranges are refused regardless of a bot's own declared egress — even
// wildcard, which is exactly the case an SSRF through a bot that fetches
// user-supplied URLs (competitor-watch) would need to bypass.
func TestLoopbackMetadataAndPrivateRangesAreAlwaysRefused(t *testing.T) {
	for _, tc := range []struct {
		name string
		url  string
	}{
		{"loopback v4", "http://127.0.0.1:7474/api/status"},
		{"loopback v4 non-default port", "http://127.0.0.1:9999/"},
		{"loopback v6", "http://[::1]:7474/"},
		{"cloud metadata (AWS/GCP/Azure all use this address)", "http://169.254.169.254/latest/meta-data/"},
		{"private 10/8", "http://10.0.0.5/"},
		{"private 172.16/12", "http://172.20.1.1/"},
		{"private 192.168/16", "http://192.168.1.1/"},
		{"unspecified", "http://0.0.0.0/"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Wildcard: the most permissive declaration a bot can make, and
			// still refused — the whole point of this being a floor rather
			// than part of the allowlist logic.
			if err := (EgressPolicy{Allow: []string{wildcard}}).Check(tc.url); err == nil {
				t.Errorf("%s was allowed under a wildcard declaration", tc.url)
			}
			// Explicitly declaring the exact IP doesn't help either — this
			// isn't a promise a bot's own guardrails can opt back into.
			if err := (EgressPolicy{Allow: []string{"127.0.0.1", "169.254.169.254", "10.0.0.5",
				"172.20.1.1", "192.168.1.1", "0.0.0.0", "::1"}}).Check(tc.url); err == nil {
				t.Errorf("%s was allowed even when explicitly declared", tc.url)
			}
		})
	}
}

// A public IP literal is unaffected by the internal-address floor — this
// is about the specific reserved ranges, not IP literals in general.
func TestAPublicIPLiteralIsNotBlockedByTheInternalFloor(t *testing.T) {
	if err := (EgressPolicy{Allow: []string{"8.8.8.8"}}).Check("http://8.8.8.8/"); err != nil {
		t.Errorf("a public IP, explicitly allowed, was refused: %v", err)
	}
}

// The refusal has to name what the bot promised. "Refused" alone leaves
// someone guessing which of two lists they need to change.
func TestTheRefusalNamesTheBotsOwnList(t *testing.T) {
	err := EgressPolicy{Allow: []string{"googleapis.com"}}.Check("https://evil.example/x")
	if err == nil {
		t.Fatal("expected a refusal")
	}
	for _, want := range []string{"googleapis.com", "evil.example", "network_egress"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not mention %q: %v", want, err)
		}
	}
}

// The check and the fetch must look at the same URLs, or a list-shaped
// fetch slips past a check that only looked at params.url.
func TestEveryURLInAListIsChecked(t *testing.T) {
	got := fetchURLs(map[string]any{"urls": []any{"https://a.example", "https://b.example"}})
	if len(got) != 2 {
		t.Fatalf("got %v", got)
	}
	if one := fetchURLs(map[string]any{"url": "https://c.example"}); len(one) != 1 {
		t.Errorf("got %v", one)
	}
	if none := fetchURLs(map[string]any{}); len(none) != 0 {
		t.Errorf("got %v", none)
	}
}

func TestLiveDepsRefusesAFetchOutsideTheAllowlist(t *testing.T) {
	l := &LiveDeps{Demo: NewDemoDeps(t.TempDir(), nil), Egress: EgressPolicy{Allow: []string{"example.com"}}}
	// A list where only the second URL is out of bounds: the whole call
	// must be refused before any of it is fetched.
	_, err := l.WebFetch(map[string]any{"urls": []any{"https://example.com/a", "https://evil.example/b"}})
	if err == nil {
		t.Fatal("a disallowed URL in a list was fetched")
	}
	if !strings.Contains(err.Error(), "evil.example") {
		t.Errorf("err = %v", err)
	}
}
