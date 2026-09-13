package share

import (
	"strings"
	"testing"

	"github.com/redbotster/nanobots/internal/schema"
)

func bot(services []schema.Service, writes []string) *schema.Nanobot {
	return &schema.Nanobot{
		Spec: schema.NanobotSpec{
			Services:   services,
			Guardrails: schema.Guardrails{WritesAllowed: writes},
		},
	}
}

func swarm(bots ...schema.BotRef) *schema.Nanoswarm {
	return &schema.Nanoswarm{
		Metadata: schema.Metadata{Name: "probe"},
		Spec:     schema.NanoswarmSpec{Bots: bots},
	}
}

// A bundle names its bots rather than carrying them: they are catalog
// entries with versions, and shipping copies would fork them silently.
func TestABundleNamesTheBotsItNeeds(t *testing.T) {
	sw := swarm(
		schema.BotRef{ID: "a", Use: "invoice-chaser@0.1.0"},
		schema.BotRef{ID: "b", Use: "notify@0.1.0"},
		schema.BotRef{ID: "c", Use: "notify@0.1.0"}, // same bot twice
	)
	b, err := Export([]byte("swarm: yaml"), sw, func(string) (*schema.Nanobot, error) {
		return bot(nil, nil), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Requires) != 2 {
		t.Fatalf("requires = %v, want two distinct", b.Requires)
	}
	if b.Requires[0] != "invoice-chaser@0.1.0" {
		t.Errorf("not sorted: %v", b.Requires)
	}
	// The file itself is carried verbatim: every catalog swarm's header
	// explains the choices it made, and that is most of what a reader
	// needs.
	if b.Swarm != "swarm: yaml" {
		t.Errorf("swarm body = %q", b.Swarm)
	}
}

// A swarm is executable — get-paid emails your customers — so handing one
// over should say what it can do before it runs, not after.
func TestABundleSaysWhatTheSwarmCanWriteTo(t *testing.T) {
	sw := swarm(schema.BotRef{ID: "s", Use: "email-send-approved@0.1.0"})
	b, _ := Export(nil, sw, func(string) (*schema.Nanobot, error) {
		return bot([]schema.Service{{Provider: "google", Connection: schema.ConnectionOAuthNative}}, []string{"gmail"}), nil
	})
	if len(b.Acts) != 1 || b.Acts[0] != "gmail" {
		t.Errorf("acts = %v", b.Acts)
	}
	if len(b.Connects) != 1 || b.Connects[0] != "google" {
		t.Errorf("connects = %v", b.Connects)
	}

	body, err := b.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	// Loud in the file, for someone who opens it in an editor rather than
	// through the app.
	if !strings.Contains(string(body), "CAN WRITE TO: gmail") {
		t.Errorf("the warning is not in the file:\n%s", body)
	}
	if !strings.Contains(string(body), "no credentials") {
		t.Error("the file does not say it carries no credentials")
	}
}

// Demo services need nothing connected — that is the point of demo mode,
// and listing them would make every bundle look like it wants your Gmail.
func TestADemoOnlySwarmAsksForNothing(t *testing.T) {
	sw := swarm(schema.BotRef{ID: "t", Use: "inbox-triage@0.1.0"})
	b, _ := Export(nil, sw, func(string) (*schema.Nanobot, error) {
		return bot([]schema.Service{{Provider: "google", Connection: schema.ConnectionDemo}}, nil), nil
	})
	if len(b.Connects) != 0 {
		t.Errorf("connects = %v, want none", b.Connects)
	}
}

// A `path:` bot lives only on the machine that wrote it, so exporting a
// reference that cannot resolve would produce a bundle guaranteed to fail
// on arrival.
func TestALocalPathBotCannotBeShared(t *testing.T) {
	sw := swarm(schema.BotRef{ID: "mine", Path: "./scratch/my-bot"})
	_, err := Export(nil, sw, func(string) (*schema.Nanobot, error) { return bot(nil, nil), nil })
	if err == nil {
		t.Fatal("exported an unresolvable reference")
	}
	if !strings.Contains(err.Error(), "mine") || !strings.Contains(err.Error(), "local path") {
		t.Errorf("err = %v", err)
	}
}

// Everything is checked before anything is written: a swarm referencing a
// bot you don't have is not importable, and finding out at run time — after
// it is saved and looks legitimate — is the worse order.
func TestMissingBotsAreListedBeforeAnythingIsWritten(t *testing.T) {
	b := &Bundle{Format: 1, Swarm: "x", Requires: []string{"have@1", "gone@1", "also-gone@2"}}
	missing := b.Missing(func(use string) bool { return use == "have@1" })
	if len(missing) != 2 {
		t.Fatalf("missing = %v", missing)
	}
}

// The likeliest mistake is handing `import` a plain swarm file, so that
// gets named rather than reported as "format is 0".
func TestAPlainSwarmFileIsRejectedByName(t *testing.T) {
	_, err := Parse([]byte("apiVersion: nanobots.dev/v1alpha1\nkind: Nanoswarm\nmetadata:\n  name: x\n"))
	if err == nil {
		t.Fatal("accepted a plain swarm as a bundle")
	}
	if !strings.Contains(err.Error(), "plain swarm YAML") {
		t.Errorf("err = %v", err)
	}
}

// A newer bundle must say so rather than be half-read.
func TestAFutureFormatIsRefused(t *testing.T) {
	_, err := Parse([]byte("format: 99\nswarm: |\n  x\n"))
	if err == nil || !strings.Contains(err.Error(), "upgrade") {
		t.Errorf("err = %v", err)
	}
}

func TestRoundTrip(t *testing.T) {
	sw := swarm(schema.BotRef{ID: "a", Use: "notify@0.1.0"})
	orig, err := Export([]byte("the: swarm\n"), sw, func(string) (*schema.Nanobot, error) {
		return bot(nil, []string{"slack"}), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	body, err := orig.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	back, err := Parse(body)
	if err != nil {
		t.Fatal(err)
	}
	if back.Swarm != "the: swarm\n" || back.Name != "probe" || len(back.Requires) != 1 {
		t.Errorf("round trip lost something: %+v", back)
	}
	if len(back.Acts) != 1 || back.Acts[0] != "slack" {
		t.Errorf("the warning did not survive: %v", back.Acts)
	}
}
