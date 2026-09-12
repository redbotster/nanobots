package runner

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

// harnessBuild maps a nanobot.yaml harness.type to the local image tag and
// the non-root UID the image runs as (distroless nonroot is 65532; the
// openclaw image's own `nanobot` user is 10001 — see harness/*/Dockerfile).
// There's no remote registry yet (blueprint §4 #8), so nanobotd builds these
// itself from the harness/ Dockerfiles on first use rather than pulling
// nb.Spec.Resources.Image from somewhere that doesn't exist.
var harnessBuild = map[string]struct {
	Tag        string
	Dockerfile string
	User       string
}{
	"bare": {Tag: "nanobots/harness-bare:local", Dockerfile: "harness/bare/Dockerfile", User: "65532:65532"},
	// llm runs in the *same* image as bare, deliberately. ai.generate is an
	// HTTP callback to nanobotd — the container never talks to a model — so
	// a bot that generates text needs nothing beyond the interpreter and a
	// CA bundle. The value exists to describe the bot honestly, not to add
	// anything to its runtime.
	"llm":      {Tag: "nanobots/harness-bare:local", Dockerfile: "harness/bare/Dockerfile", User: "65532:65532"},
	"openclaw": {Tag: "nanobots/harness-openclaw:local", Dockerfile: "harness/openclaw/Dockerfile", User: "10001:10001"},
}

// EnsureHarnessImage builds the harness image for harnessType if it isn't
// already present locally, returning its tag and run-as user. There's no
// content-hash check against internal/step et al — a harness image is only
// as fresh as the last time it was built, so it's a real, known trap for
// anyone iterating on the interpreter or a bot's Go-side dependencies while
// testing swarms locally. Set NANOBOTS_REBUILD_HARNESS=1 to always rebuild
// rather than trusting whatever's cached (or just `docker rmi` the tag).
func EnsureHarnessImage(harnessType, repoRoot string) (tag, user string, err error) {
	h, ok := harnessBuild[harnessType]
	if !ok {
		return "", "", fmt.Errorf("harness %q is not implemented in this build (only bare, llm, openclaw)", harnessType)
	}
	forceRebuild := os.Getenv("NANOBOTS_REBUILD_HARNESS") != ""

	// The agent binary is baked into the image, so an image built before an
	// edit to internal/step or cmd/nanobot-agent silently runs the old
	// interpreter. This used to be a documented trap ("only as fresh as the
	// last time it was built") and it cost real debugging time: a 36-hour-old
	// image made a prompt-safety change look verified when the container was
	// running code that predated it. The sources are hashed into a label at
	// build time and compared here, so staleness is detected rather than
	// remembered.
	want := agentSourceHash(repoRoot)
	if !forceRebuild && want != "" {
		out, err := exec.Command("docker", "image", "inspect",
			"--format", "{{index .Config.Labels \"nanobots.agent-source\"}}", h.Tag).Output()
		if err == nil && strings.TrimSpace(string(out)) == want {
			return h.Tag, h.User, nil // built from exactly this source
		}
	} else if !forceRebuild {
		// Couldn't hash (unreadable tree) — fall back to the old
		// "exists is good enough" rule rather than rebuilding every run.
		if err := exec.Command("docker", "image", "inspect", h.Tag).Run(); err == nil {
			return h.Tag, h.User, nil
		}
	}
	build := exec.Command("docker", "build", "-f", h.Dockerfile,
		"--label", "nanobots.agent-source="+want, "-t", h.Tag, repoRoot)
	var stderr bytes.Buffer
	build.Stderr = &stderr
	if err := build.Run(); err != nil {
		return "", "", fmt.Errorf("build harness image %s: %w: %s", h.Tag, err, stderr.String())
	}
	return h.Tag, h.User, nil
}

// DockerAvailable reports whether the docker daemon is reachable right now,
// and if it isn't, the one-line reason.
//
// Every bot runs in a container, so a stopped Docker Desktop makes the whole
// product fail — and it used to fail late, deep inside a run, as a wall of
// "cannot connect to the Docker daemon at unix://..." in a log nobody opens.
// Asking up front costs one cheap command and lets the UI say so before you
// hit Run. `docker version` (not `info`) because it's the fastest call that
// still round-trips to the daemon rather than answering from the client.
func DockerAvailable() (bool, string) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", "version", "--format", "{{.Server.Version}}")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return false, "Docker didn't respond within 3s"
		}
		if _, lookErr := exec.LookPath("docker"); lookErr != nil {
			return false, "Docker isn't installed"
		}
		return false, "Docker isn't running"
	}
	return true, ""
}

// ContainerSpec is what RunContainer needs to run one bot instance.
type ContainerSpec struct {
	Image  string
	User   string // "uid:gid"
	BotDir string // host path, mounted read-only at /bot
	RunDir string // host path, mounted read-write at /run
	// BlobDir is nanobotd's own blob store, mounted read-only so a bot can
	// read a file an earlier bot produced. Read-only on purpose: a bot
	// needs to see upstream files, never to alter them. Empty skips it.
	BlobDir    string
	Env        map[string]string
	MaxRuntime time.Duration // 0 means use a conservative default
}

func dockerRunArgs(spec ContainerSpec, name string) []string {
	args := []string{
		"run", "--rm",
		// Named so the timeout path can actually stop it — see RunContainer.
		"--name", name,
		"--read-only",
		"--tmpfs", "/tmp:size=256m",
		"--network", "bridge", // outbound only; see guardrails.network_egress TODO below
		// host.docker.internal resolves out of the box on Docker Desktop
		// (macOS/Windows); this flag is what makes it resolve on Linux too.
		"--add-host", "host.docker.internal:host-gateway",
		"--user", spec.User,
		"-v", spec.BotDir + ":/bot:ro",
		"-v", spec.RunDir + ":/run",
	}
	if spec.BlobDir != "" {
		args = append(args, "-v", spec.BlobDir+":/blobs:ro")
	}
	for k, v := range spec.Env {
		args = append(args, "-e", k+"="+v)
	}
	// TODO(nanobots#egress): guardrails.network_egress isn't enforced as an
	// actual container network policy yet — blueprint §3.3 calls this out
	// explicitly as "reported, not enforced" until it's built.
	return append(args, spec.Image)
}

// RunContainer runs one bot instance to completion, bounded by MaxRuntime
// (defaulting to 5 minutes if unset — comfortably above any example bot's
// own max_runtime_secs guardrail, since that guardrail is the bot's own
// promise, not the outer safety net).
func RunContainer(spec ContainerSpec) (exitCode int, stderr string, err error) {
	timeout := spec.MaxRuntime
	if timeout == 0 {
		timeout = 5 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	name := "nanobot-" + uuid.NewString()
	cmd := exec.CommandContext(ctx, "docker", dockerRunArgs(spec, name)...)
	var errBuf bytes.Buffer
	cmd.Stderr = &errBuf
	runErr := cmd.Run()

	if ctx.Err() == context.DeadlineExceeded {
		// exec.CommandContext SIGKILLs the docker *CLI*, which does nothing
		// to the container the daemon is running — verified against Docker
		// 29.2.1: the container is still Up seconds after the client dies.
		// So the old "was killed" message was simply false, and a hung bot
		// kept burning CPU, holding its network egress, and writing to the
		// bind-mounted /run directory of a run already marked failed.
		// Repeated timeouts accumulated orphans until the host gave out.
		//
		// Stopping it needs a separate command against the daemon, on its
		// own context since the original one is already expired.
		killCtx, killCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer killCancel()
		killErr := exec.CommandContext(killCtx, "docker", "kill", name).Run()
		if killErr != nil {
			return -1, errBuf.String(), fmt.Errorf(
				"container exceeded %s, and stopping it failed (%v) — it may still be running as %s",
				timeout, killErr, name)
		}
		return -1, errBuf.String(), fmt.Errorf("container exceeded %s and was stopped", timeout)
	}
	if runErr != nil {
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			return exitErr.ExitCode(), errBuf.String(), fmt.Errorf("container exited %d: %s", exitErr.ExitCode(), errBuf.String())
		}
		return -1, errBuf.String(), fmt.Errorf("run container: %w", runErr)
	}
	return 0, errBuf.String(), nil
}

// agentSourceHash fingerprints everything that ends up in the agent binary:
// its own package, the interpreter and everything it imports, and the module
// files. Returns "" if the tree can't be read, which callers treat as "can't
// tell" rather than "stale".
func agentSourceHash(repoRoot string) string {
	h := sha256.New()
	roots := []string{
		filepath.Join(repoRoot, "cmd", "nanobot-agent"),
		filepath.Join(repoRoot, "internal"),
	}
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			// _test.go files never reach the binary, and including them
			// would rebuild the image every time a test changed.
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(repoRoot, path)
			fmt.Fprintf(h, "%s\n", rel)
			h.Write(data)
			return nil
		})
		if err != nil {
			return ""
		}
	}
	for _, f := range []string{"go.mod", "go.sum"} {
		data, err := os.ReadFile(filepath.Join(repoRoot, f))
		if err != nil {
			return ""
		}
		h.Write(data)
	}
	return hex.EncodeToString(h.Sum(nil))
}
