package runner

import (
	"strings"
	"testing"
)

func TestDockerRunArgsIncludesSecurityFlags(t *testing.T) {
	spec := ContainerSpec{
		Image:  "nanobots/harness-bare:local",
		User:   "65532:65532",
		BotDir: "/repo/bots/email-drive-file",
		RunDir: "/tmp/run-1",
		Env:    map[string]string{"NANOBOTS_CALLBACK_URL": "http://host.docker.internal:7474"},
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
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("docker run args missing %q\ngot: %s", want, joined)
		}
	}
	if args[len(args)-1] != spec.Image {
		t.Errorf("image should be the last arg, got %q", args[len(args)-1])
	}
}

func TestEnsureHarnessImageRejectsUnknownHarness(t *testing.T) {
	if _, _, err := EnsureHarnessImage("hermes", "."); err == nil {
		t.Fatal("expected an error for a harness this build doesn't implement")
	}
}
