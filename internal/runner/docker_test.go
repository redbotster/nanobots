package runner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDockerRunArgsIncludesSecurityFlags(t *testing.T) {
	spec := ContainerSpec{
		Image:   "nanobots/harness-bare:local",
		User:    "65532:65532",
		BotDir:  "/repo/bots/email-drive-file",
		RunDir:  "/tmp/run-1",
		BlobDir: "/home/me/.nanobots/blobs",
		Env:     map[string]string{"NANOBOTS_CALLBACK_URL": "http://host.docker.internal:7474"},
	}
	args := dockerRunArgs(spec, "nanobot-test-1")
	joined := strings.Join(args, " ")

	for _, want := range []string{
		"--read-only",
		"--user 65532:65532",
		"-v /repo/bots/email-drive-file:/bot:ro",
		"-v /tmp/run-1:/run",
		"-e NANOBOTS_CALLBACK_URL=http://host.docker.internal:7474",
		// Named so the MaxRuntime path can stop the container itself.
		// Without this, killing the docker CLI left the container running.
		"--name nanobot-test-1",
		// The host blob store, read-only, so a bot can read a file an
		// earlier bot produced without being able to alter it.
		"-v /home/me/.nanobots/blobs:/blobs:ro",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("docker run args missing %q\ngot: %s", want, joined)
		}
	}
	if args[len(args)-1] != spec.Image {
		t.Errorf("image should be the last arg, got %q", args[len(args)-1])
	}

	// The default: a bridge, and a route to the host, because almost every
	// bot calls back to nanobotd for anything that touches a credential.
	if !strings.Contains(joined, "--network bridge") {
		t.Errorf("a bot that calls back needs a network\ngot: %s", joined)
	}
	if !strings.Contains(joined, "--add-host host.docker.internal:host-gateway") {
		t.Errorf("without host-gateway a callback cannot resolve the host on Linux\ngot: %s", joined)
	}
}

// The one part of guardrails.network_egress Docker can enforce on its own.
//
// A bot that declares no egress and calls nothing gets no interface, so the
// Chromium inside it cannot fetch a remote asset either — the hole the TODO
// in dockerRunArgs describes, closed where closing it costs nothing.
// `render-pdf` is that bot, and it really does render this way: verified in
// a real container, not only here.
func TestDockerRunArgsGiveAnOfflineBotNoNetwork(t *testing.T) {
	spec := ContainerSpec{
		Image:     "nanobots/harness-openclaw:local",
		User:      "65532:65532",
		BotDir:    "/repo/bots/render-pdf",
		RunDir:    "/tmp/run-1",
		NoNetwork: true,
	}
	joined := strings.Join(dockerRunArgs(spec, "nanobot-test-2"), " ")

	if !strings.Contains(joined, "--network none") {
		t.Errorf("an offline bot still got a network\ngot: %s", joined)
	}
	if strings.Contains(joined, "--network bridge") {
		t.Errorf("both networks requested at once\ngot: %s", joined)
	}
	// Pointless with no interface, and its presence would suggest the
	// container can still reach the host.
	if strings.Contains(joined, "host-gateway") {
		t.Errorf("a route to the host on a container with no network\ngot: %s", joined)
	}
	// Everything else it had, it keeps.
	if !strings.Contains(joined, "--read-only") || !strings.Contains(joined, "-v /tmp/run-1:/run") {
		t.Errorf("the rest of the sandbox went missing\ngot: %s", joined)
	}
}

func TestEnsureHarnessImageRejectsUnknownHarness(t *testing.T) {
	if _, _, err := EnsureHarnessImage("hermes", "."); err == nil {
		t.Fatal("expected an error for a harness this build doesn't implement")
	}
}

// The agent binary is baked into the harness image, so an image built
// before an edit to the interpreter silently runs old code. That cost real
// debugging time — a 36-hour-old image made a prompt-safety change look
// verified when the container predated it.
func TestAgentSourceHashChangesWithTheInterpreter(t *testing.T) {
	root := repoRootForTest(t)
	first := agentSourceHash(root)
	if first == "" {
		t.Fatal("could not hash the agent's sources")
	}
	if len(first) != 64 {
		t.Errorf("hash = %q, want a sha256 hex digest", first)
	}
	// Stable across calls, or every run would rebuild.
	if second := agentSourceHash(root); second != first {
		t.Errorf("hash is not stable: %s != %s", first, second)
	}

	// A change to a file that reaches the binary must change it.
	probe := filepath.Join(root, "internal", "step", "zz_hash_probe.go")
	if err := os.WriteFile(probe, []byte("package step\n\nconst zzHashProbe = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(probe)
	if changed := agentSourceHash(root); changed == first {
		t.Error("editing internal/step did not change the hash; a stale image would go undetected")
	}
	os.Remove(probe)

	// A test file does not reach the binary, and must not force a rebuild.
	tprobe := filepath.Join(root, "internal", "step", "zz_hash_probe_test.go")
	if err := os.WriteFile(tprobe, []byte("package step\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tprobe)
	if withTest := agentSourceHash(root); withTest != first {
		t.Error("adding a _test.go file changed the hash; every test edit would rebuild the image")
	}
}
