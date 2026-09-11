package foundry

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

const foundryAgentImage = "nanobots/foundry-agent:local"

// ensureFoundryAgentImage builds the foundry's agent sandbox image if it
// isn't already present locally — the same build-if-missing pattern
// internal/runner.EnsureHarnessImage already uses for the bot harness
// images, including the same real, known trap: this doesn't content-hash
// the Dockerfile, so it's only as fresh as the last build. `docker rmi
// nanobots/foundry-agent:local` to force a rebuild.
func ensureFoundryAgentImage(repoRoot string) (string, error) {
	check := exec.Command("docker", "image", "inspect", foundryAgentImage)
	if err := check.Run(); err == nil {
		return foundryAgentImage, nil
	}
	build := exec.Command("docker", "build", "-f", filepath.Join(repoRoot, "harness", "foundry-agent", "Dockerfile"), "-t", foundryAgentImage, repoRoot)
	var stderr bytes.Buffer
	build.Stderr = &stderr
	if err := build.Run(); err != nil {
		return "", fmt.Errorf("build %s: %w: %s", foundryAgentImage, err, stderr.String())
	}
	return foundryAgentImage, nil
}

// dockerMount is one bind mount for a sandboxed agent container.
type dockerMount struct {
	HostPath      string
	ContainerPath string
	ReadOnly      bool
}

// dockerAgentSpec is everything runDockerAgent needs to run one coding-agent
// CLI inside its sandbox — the seam a future adapter for a different CLI
// (e.g. OpenCode) would build its own values for, calling the exact same
// runDockerAgent underneath rather than reimplementing container
// plumbing per tool. Only ClaudeCLIAgent (agent_claude.go) is shipped; this
// struct and runDockerAgent are what a second adapter would reuse.
type dockerAgentSpec struct {
	Image  string
	Args   []string // entrypoint args, e.g. ["-p", "--output-format", "stream-json", ...]
	Env    map[string]string
	Mounts []dockerMount
}

// runDockerAgent runs one coding-agent CLI inside a container — confined by
// the container boundary itself, not by the CLI's own permission flags (see
// agent.go's package doc for why that distinction is load-bearing here).
// stdout is scanned line by line and handed to parseLine, whose results
// stream onto events live, the same way a real terminal session would
// render progress. Unlike internal/runner/docker.go's RunContainer (which
// buffers a bot's output and replays it after the container exits), this
// needs genuinely live streaming — the whole point of a human watching a
// foundry job work.
func runDockerAgent(ctx context.Context, spec dockerAgentSpec, stdin string, events chan<- Event, parseLine func([]byte) []Event) error {
	args := []string{
		"run", "--rm", "-i",
		"--network", "bridge", // this container, uniquely, needs real outbound access — see harness/foundry-agent/Dockerfile's doc comment
	}
	for _, m := range spec.Mounts {
		flag := m.HostPath + ":" + m.ContainerPath
		if m.ReadOnly {
			flag += ":ro"
		}
		args = append(args, "-v", flag)
	}
	for k, v := range spec.Env {
		args = append(args, "-e", k+"="+v)
	}
	args = append(args, spec.Image)
	args = append(args, spec.Args...)

	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Stdin = strings.NewReader(stdin)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start agent container: %w", err)
	}

	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 0, 64*1024), 4<<20) // a stream-json line can carry a full tool input/output
	for sc.Scan() {
		for _, ev := range parseLine(sc.Bytes()) {
			select {
			case events <- ev:
			case <-ctx.Done():
			}
		}
	}

	err = cmd.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return fmt.Errorf("agent container exited with an error: %w: %s", err, stderr.String())
	}
	return nil
}
