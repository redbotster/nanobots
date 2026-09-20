package contract

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// A standalone binary with no harness/ source pulls a published image
// instead of building one — internal/runner.EnsureHarnessImage names it
// ghcr.io/redbotster/nanobots-harness-<type>, and
// .github/workflows/release.yml is what has to actually publish it there.
// Two files, one fact: this reads both and requires they agree, the same
// way TestTheNpxShimAsksForAnAssetTheReleaseActuallyBuilds already does
// for the npm shim and GoReleaser's own asset names.
func TestTheHarnessRegistryNamesMatchWhatReleaseActuallyPublishes(t *testing.T) {
	root := repoRoot(t)

	built := harnessBuildRegistryImages(t, root)
	published := releaseWorkflowHarnessMatrix(t, root)

	if len(built) == 0 {
		t.Fatal("found no RegistryImage entries in internal/runner/docker.go")
	}
	if len(published) == 0 {
		t.Fatal("found no harness matrix in .github/workflows/release.yml")
	}

	for harness, image := range built {
		want, ok := published[harness]
		if !ok {
			t.Errorf("docker.go declares a %s harness image (%s), but "+
				".github/workflows/release.yml's matrix does not build one for %q — "+
				"a standalone binary would try to pull an image nothing ever publishes",
				harness, image, harness)
			continue
		}
		if image != want {
			t.Errorf("%s: docker.go says %q, release.yml publishes %q", harness, image, want)
		}
	}
}

var harnessEntryPattern = regexp.MustCompile(`(?m)"(\w+)":\s*\{[^}]*RegistryImage:\s*"([^"]+)"`)

// harnessBuildRegistryImages reads the real harnessBuild map literal out of
// internal/runner/docker.go, rather than a copy of it living in this test.
func harnessBuildRegistryImages(t *testing.T, root string) map[string]string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, "internal", "runner", "docker.go"))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, m := range harnessEntryPattern.FindAllStringSubmatch(string(raw), -1) {
		out[m[1]] = m[2]
	}
	return out
}

var harnessMatrixPattern = regexp.MustCompile(`harness:\s*\[([^\]]+)\]`)

// releaseWorkflowHarnessMatrix reads the harness-images job's matrix and
// its images: template out of the real workflow file, and expands each
// matrix value into the image name that template would actually produce —
// the same "render the real template" approach
// TestTheNpxShimAsksForAnAssetTheReleaseActuallyBuilds takes with
// .goreleaser.yaml's name_template.
func releaseWorkflowHarnessMatrix(t *testing.T, root string) map[string]string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "release.yml"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(raw)

	m := harnessMatrixPattern.FindStringSubmatch(src)
	if m == nil {
		return nil
	}
	var harnesses []string
	for _, h := range strings.Split(m[1], ",") {
		harnesses = append(harnesses, strings.TrimSpace(h))
	}

	imagesLine := regexp.MustCompile(`images:\s*ghcr\.io/\$\{\{\s*github\.repository_owner\s*\}\}/nanobots-harness-\$\{\{\s*matrix\.harness\s*\}\}`)
	if !imagesLine.MatchString(src) {
		t.Fatal("the harness-images job's `images:` line does not match the expected " +
			"ghcr.io/${{ github.repository_owner }}/nanobots-harness-${{ matrix.harness }} template — " +
			"update this test's expansion alongside whatever it was changed to")
	}

	out := map[string]string{}
	for _, h := range harnesses {
		out[h] = "ghcr.io/redbotster/nanobots-harness-" + h
	}
	// llm shares bare's image (see docker.go's own comment on why) and has
	// no matrix entry of its own — that's expected, not a gap.
	if _, ok := out["bare"]; ok {
		out["llm"] = out["bare"]
	}
	return out
}
