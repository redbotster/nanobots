package step

import (
	"testing"

	"github.com/redbotster/nanobots/internal/schema"
)

// A fresh install — no 1Claw, no model key — runs entirely on DemoDeps, so
// every service call is a fixture. That is the configuration where the
// "DEMO DATA" banner matters most, and it was the one configuration that
// never showed it: only LiveDeps reported its calls, so the run recorded no
// demo services and the UI had nothing to render.
func TestDemoDepsReportsEveryServiceCallAsDemo(t *testing.T) {
	d := NewDemoDeps(t.TempDir(), nil)
	var seen []string
	d.OnServiceCall = func(svc schema.Service, op string, demo bool) {
		if !demo {
			t.Errorf("DemoDeps reported %s.%s as not demo — everything here is demo", svc.ID, op)
		}
		seen = append(seen, svc.ID+"."+op)
	}
	// The fixtures do not exist; the call still has to be reported, because
	// a missing fixture is a different problem from a call that happened.
	_, _ = d.ServiceCall(schema.Service{ID: "gmail", Provider: "google"}, "messages.list", nil)
	_, _ = d.ServiceCall(schema.Service{ID: "calendar", Provider: "google"}, "events.list", nil)

	if len(seen) != 2 || seen[0] != "gmail.messages.list" || seen[1] != "calendar.events.list" {
		t.Errorf("reported %v", seen)
	}
}

// The hook is optional; nothing should require it.
func TestDemoDepsWorksWithoutTheHook(t *testing.T) {
	d := NewDemoDeps(t.TempDir(), nil)
	if _, err := d.ServiceCall(schema.Service{ID: "gmail"}, "messages.list", nil); err == nil {
		t.Skip("fixture unexpectedly present")
	}
}
