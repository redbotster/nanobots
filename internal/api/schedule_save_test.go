package api

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/redbotster/nanobots/internal/schema"
)

func strptr(s string) *string { return &s }

// The gap this closes: compose "every friday summarise my overdue invoices"
// and you got a swarm the model had described as weekly that would never
// once fire on a Friday.
func TestSavingANewSwarmWithAScheduleWritesACronTrigger(t *testing.T) {
	srv := testServerWithTempSwarms(t)
	rec := postJSON(t, srv, "/api/swarms", saveSwarmRequest{
		Name:     "weekly-probe",
		Bots:     []builderBotRef{{ID: "n", Use: "notify@0.1.0"}},
		Schedule: strptr("0 9 * * 5"),
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d: %s", rec.Code, rec.Body.String())
	}
	var out saveSwarmResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	sw, err := schema.LoadNanoswarm(filepath.Join(srv.SwarmsDir, "weekly-probe.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if sw.Spec.Trigger.Type != "cron" || sw.Spec.Trigger.Expr != "0 9 * * 5" {
		t.Errorf("trigger = %+v, want a cron trigger", sw.Spec.Trigger)
	}
}

// A schedule the scheduler cannot parse is refused before anything is
// written. Saved, it would look scheduled in the list, report no next run,
// and silently never happen — worse than being manual.
func TestSavingRefusesAScheduleThatCouldNeverFire(t *testing.T) {
	srv := testServerWithTempSwarms(t)
	rec := postJSON(t, srv, "/api/swarms", saveSwarmRequest{
		Name:     "bad-probe",
		Bots:     []builderBotRef{{ID: "n", Use: "notify@0.1.0"}},
		Schedule: strptr("every friday please"),
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code = %d, want 400: %s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(filepath.Join(srv.SwarmsDir, "bad-probe.yaml")); !os.IsNotExist(err) {
		t.Error("a swarm with an unrunnable schedule was written anyway")
	}
}

// The one that would be a data-loss bug: a save that says nothing about the
// schedule must leave an existing one alone. The builder does not model the
// trigger, so every edit from it sends no schedule at all.
func TestEditingASwarmWithoutMentioningTheScheduleKeepsIt(t *testing.T) {
	srv := testServerWithTempSwarms(t)
	path := filepath.Join(srv.SwarmsDir, "scheduled.yaml")
	if err := os.WriteFile(path, []byte(`apiVersion: nanobots.dev/v1alpha1
kind: Nanoswarm
metadata:
  name: scheduled
  description: probe
spec:
  trigger:
    type: cron
    expr: "0 8 * * 1"
    timezone: America/Los_Angeles
  bots:
    - id: n
      use: notify@0.1.0
  snaps: []
`), 0o600); err != nil {
		t.Fatal(err)
	}

	rec := postJSON(t, srv, "/api/swarms", saveSwarmRequest{
		Path: path,
		Name: "scheduled",
		Bots: []builderBotRef{{ID: "n", Use: "notify@0.1.0"}},
		// No Schedule field at all — the builder's normal save.
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d: %s", rec.Code, rec.Body.String())
	}
	sw, err := schema.LoadNanoswarm(path)
	if err != nil {
		t.Fatal(err)
	}
	if sw.Spec.Trigger.Type != "cron" || sw.Spec.Trigger.Expr != "0 8 * * 1" {
		t.Errorf("editing destroyed the schedule: %+v", sw.Spec.Trigger)
	}
	if sw.Spec.Trigger.Timezone != "America/Los_Angeles" {
		t.Errorf("editing dropped the timezone: %q", sw.Spec.Trigger.Timezone)
	}
}

// Present-and-empty is the deliberate "make this manual", distinct from
// absent.
func TestAnEmptyScheduleTurnsAnExistingCronSwarmManual(t *testing.T) {
	srv := testServerWithTempSwarms(t)
	path := filepath.Join(srv.SwarmsDir, "unschedule.yaml")
	if err := os.WriteFile(path, []byte(`apiVersion: nanobots.dev/v1alpha1
kind: Nanoswarm
metadata:
  name: unschedule
spec:
  trigger:
    type: cron
    expr: "0 8 * * 1"
  bots:
    - id: n
      use: notify@0.1.0
  snaps: []
`), 0o600); err != nil {
		t.Fatal(err)
	}
	rec := postJSON(t, srv, "/api/swarms", saveSwarmRequest{
		Path: path, Name: "unschedule",
		Bots:     []builderBotRef{{ID: "n", Use: "notify@0.1.0"}},
		Schedule: strptr(""),
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d: %s", rec.Code, rec.Body.String())
	}
	sw, err := schema.LoadNanoswarm(path)
	if err != nil {
		t.Fatal(err)
	}
	if sw.Spec.Trigger.Type != "manual" {
		t.Errorf("trigger type = %q, want manual", sw.Spec.Trigger.Type)
	}
	if sw.Spec.Trigger.Expr != "" {
		t.Errorf("the old cron expression survived: %q", sw.Spec.Trigger.Expr)
	}
}

// The composer is a language model writing cron by hand. The prompt has to
// actually ask for it, or none of the above ever gets exercised.
func TestTheComposePromptAsksForASchedule(t *testing.T) {
	p := composePrompt(nil, "every friday email me the overdue invoices")
	for _, want := range []string{`"schedule"`, "cron expression", "0 7 * * 1-5"} {
		if !strings.Contains(p, want) {
			t.Errorf("the compose prompt never mentions %q", want)
		}
	}
}

// A cron expression the user cannot read is a promise they cannot check.
func TestDescribeScheduleTurnsCronIntoEnglish(t *testing.T) {
	srv := testServerWithTempSwarms(t)
	for expr, want := range map[string]string{
		"0 9 * * 5":    "Fridays at 9:00 AM",
		"0 7 * * 1-5":  "Weekdays at 7:00 AM",
		"*/30 * * * *": "Every 30 minutes",
	} {
		rec := get(srv, "/api/schedule/describe?expr="+url.QueryEscape(expr))
		var got map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if got["ok"] != true || got["human"] != want {
			t.Errorf("%q => %v, want %q", expr, got, want)
		}
	}
}

// "That isn't a schedule" is the answer to the question, not a failure to
// answer it — the picker calls this as you type and a stream of 400s would
// be noise in the console.
func TestDescribeScheduleReportsABadExpressionAsAnAnswer(t *testing.T) {
	srv := testServerWithTempSwarms(t)
	rec := get(srv, "/api/schedule/describe?expr="+url.QueryEscape("every friday please"))
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", rec.Code)
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["ok"] != false {
		t.Errorf("ok = %v, want false for an unparseable expression", got["ok"])
	}
	if got["error"] == nil || got["error"] == "" {
		t.Error("no reason given for the refusal")
	}
}

// An empty expression is a real answer too: this swarm runs when you say so.
func TestDescribeScheduleExplainsTheEmptyCase(t *testing.T) {
	srv := testServerWithTempSwarms(t)
	rec := get(srv, "/api/schedule/describe?expr=")
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["ok"] != true || !strings.Contains(got["human"].(string), "press Run") {
		t.Errorf("empty expr => %v", got)
	}
}
