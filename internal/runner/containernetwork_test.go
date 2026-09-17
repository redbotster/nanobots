package runner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/redbotster/nanobots/internal/schema"
)

// The one part of guardrails.network_egress Docker can enforce by itself.
//
// An allowlist needs a per-run network and an egress proxy. "None" is a
// flag — and for a bot that declares no egress and calls nothing, none is
// the whole of its declared policy. Getting the predicate wrong in the
// permissive direction leaves today's behaviour; getting it wrong the other
// way pulls the network out from under a bot that needs it, so the list of
// local steps is deny-by-default.
func TestNeedsContainerNetwork(t *testing.T) {
	bot := func(steps []string, services int, egress []string) *schema.Nanobot {
		nb := &schema.Nanobot{}
		for _, s := range steps {
			nb.Spec.Steps = append(nb.Spec.Steps, schema.Step{Name: s, Type: s})
		}
		for i := 0; i < services; i++ {
			nb.Spec.Services = append(nb.Spec.Services, schema.Service{ID: "svc"})
		}
		nb.Spec.Guardrails.NetworkEgress = egress
		return nb
	}

	for _, tc := range []struct {
		name string
		nb   *schema.Nanobot
		want bool
	}{
		{"renders and nothing else", bot([]string{"transform.render"}, 0, nil), false},
		{"shapes a value and stops early", bot([]string{"transform.pick", "stop.if"}, 0, nil), false},
		{"calls a service", bot([]string{"service.call"}, 1, nil), true},
		{"asks a model", bot([]string{"ai.generate"}, 0, nil), true},
		{"remembers something", bot([]string{"memory.put"}, 0, nil), true},
		{"fetches a page", bot([]string{"web.fetch"}, 0, []string{"*"}), true},
		{"asks a human", bot([]string{"approve"}, 0, nil), true},
		// A declared service is a callback waiting to happen even if no
		// step uses it yet.
		{"declares a service it does not use", bot([]string{"transform.render"}, 1, nil), true},
		// A bot that says where it may go intends to go somewhere; taking
		// its network away would contradict its own declaration.
		{"declares egress but looks local", bot([]string{"transform.render"}, 0, []string{"fonts.example"}), true},
		// The direction that must fail safe: a step type this list has
		// never heard of keeps its network.
		{"an unknown step type", bot([]string{"quantum.entangle"}, 0, nil), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := needsContainerNetwork(tc.nb); got != tc.want {
				t.Errorf("needsContainerNetwork = %v, want %v", got, tc.want)
			}
		})
	}
}

// Against the real catalog, so the claim in docs/status.md is measured
// rather than asserted: exactly the bots that need nothing get nothing.
func TestTheCatalogsOfflineBotsGetNoNetwork(t *testing.T) {
	root := repoRootForTest(t)
	entries, err := os.ReadDir(filepath.Join(root, "bots"))
	if err != nil {
		t.Fatal(err)
	}
	var offline []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		nb, err := schema.LoadNanobot(filepath.Join(root, "bots", e.Name(), "nanobot.yaml"))
		if err != nil {
			t.Fatalf("load %s: %v", e.Name(), err)
		}
		if !needsContainerNetwork(nb) {
			offline = append(offline, e.Name())
		}
	}
	t.Logf("no container network: %s", strings.Join(offline, ", "))
	if len(offline) == 0 {
		t.Error("no bot in the catalog qualifies, so nothing exercises this in a real run")
	}
	for _, want := range []string{"render-pdf"} {
		if !contains(offline, want) {
			t.Errorf("%s reaches nothing and calls nothing, but would still get a network", want)
		}
	}
}

func contains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
