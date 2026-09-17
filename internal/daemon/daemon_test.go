package daemon

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// offline points HOME at a temp directory so ResolvePaths writes its
// ~/.nanobots tree there, and at an env file that does not exist so no
// credential from the developer's machine reaches the assembly.
func offline(t *testing.T) Options {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	log.SetOutput(io.Discard)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	return Options{
		RepoRoot:    "/repo",
		BotsDir:     "/repo/bots",
		EnvFilePath: filepath.Join(home, "definitely-absent.env"),
	}
}

// The assembly is a hundred lines of "this object is handed to that one",
// and the mistakes it invites are silent: a scheduler pointed at the wrong
// directory, or a breaker that is a second instance rather than the one the
// API reads. Nothing fails when that happens — the app just quietly cannot
// say why a schedule stopped.
func TestBuildWiresTheSchedulerToTheApi(t *testing.T) {
	srv, sched, opts, err := build(offline(t))
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	// One breaker, not two. The API reads it to explain a paused schedule
	// and to offer "Try it again"; the scheduler writes it.
	if srv.ScheduleBreaker == nil || sched.Breaker == nil {
		t.Fatal("the breaker is missing from one side")
	}
	if srv.ScheduleBreaker != sched.Breaker {
		t.Error("the API and the scheduler hold different breakers, so a pause the scheduler " +
			"records is one the app cannot see")
	}

	// One run store, for the same reason: a scheduled run has to appear in
	// the Runs page.
	if sched.Runs == nil || srv.Runs != sched.Runs {
		t.Error("the scheduler writes runs the API never sees")
	}
	if sched.Orchestrator == nil || srv.Orchestrator != sched.Orchestrator {
		t.Error("the scheduler runs swarms through a different orchestrator than the API does")
	}

	// Swarms live beside bots, which is how every other path here is
	// derived. Getting this wrong means the scheduler watches an empty
	// directory and nothing ever fires, silently.
	if want := "/repo/examples/swarms"; sched.SwarmsDir != want {
		t.Errorf("SwarmsDir = %q, want %q", sched.SwarmsDir, want)
	}
	if opts.Addr != "127.0.0.1:7474" {
		t.Errorf("default Addr = %q — nanobotd binds loopback only", opts.Addr)
	}
}

// A callback from inside a container is authenticated against the registry
// the orchestrator issued the token from. Two registries means every
// containerised bot's first callback is rejected.
func TestBuildSharesOneCallbackRegistry(t *testing.T) {
	srv, _, _, err := build(offline(t))
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if srv.Callbacks == nil || srv.Orchestrator.Callbacks != srv.Callbacks {
		t.Error("the API and the orchestrator hold different callback registries, so a bot's " +
			"callback would be authenticated against a token nobody issued")
	}
}

// With no 1Claw key there is nothing to bill against, so the Shroud shim is
// not exposed — offering an endpoint that cannot work is worse than not
// offering it.
func TestBuildOmitsTheShroudProxyWithoutAKey(t *testing.T) {
	srv, _, _, err := build(offline(t))
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if srv.Shroud != nil {
		t.Error("the Shroud proxy was exposed with no 1Claw key to meter against")
	}
	// And the rest of the app still assembles: a fresh install with no
	// credentials is a supported way to run this, not a degraded one.
	if srv.Orchestrator == nil || srv.Runs == nil || srv.Webhook == nil || srv.Roles == nil {
		t.Error("an unconfigured install did not assemble a working server")
	}
}

// Everything that persists goes under the home directory ResolvePaths
// chose, and nothing lands in the repo. The one file this repo has ever
// accidentally written into its own tree was a probe's secrets directory,
// and it took a test to notice.
func TestBuildWritesNothingIntoTheRepo(t *testing.T) {
	opts := offline(t)
	repo := t.TempDir()
	opts.RepoRoot = repo
	opts.BotsDir = filepath.Join(repo, "bots")

	if _, _, _, err := build(opts); err != nil {
		t.Fatalf("build: %v", err)
	}
	var found []string
	_ = filepath.Walk(repo, func(path string, info os.FileInfo, err error) error {
		if err == nil && path != repo {
			found = append(found, strings.TrimPrefix(path, repo))
		}
		return nil
	})
	if len(found) > 0 {
		t.Errorf("assembling the daemon wrote into the repo: %v", found)
	}
}

func TestPortOf(t *testing.T) {
	for addr, want := range map[string]string{
		"127.0.0.1:7474": "7474",
		"0.0.0.0:8080":   "8080",
		"[::1]:9000":     "9000",
		// The fallback matters: this port is what a container is told to
		// call back on, so a malformed address must not produce an empty
		// one and a callback URL ending in a colon.
		"garbage": "7474",
		"":        "7474",
	} {
		if got := portOf(addr); got != want {
			t.Errorf("portOf(%q) = %q, want %q", addr, got, want)
		}
	}
}
