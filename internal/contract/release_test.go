package contract

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"text/template"
)

// `npx nanobots` downloads a release asset by exact name, and the name it
// asks for has to be the name GoReleaser publishes.
//
// They did not match. GoReleaser's default template produces
// nanobots_0.1.0_darwin_amd64.tar.gz; npm/bin/nanobots.js asks for
// nanobots_0.1.0_Darwin_x86_64.tar.gz. Nobody would have found that until
// the first `npx nanobots` after the first tag, which is the worst possible
// moment: the release is public, the README says it works, and the fix is
// another release.
//
// Two files, one fact. This renders the real template from
// .goreleaser.yaml against the real platform tables from the shim and
// requires the same string out of both.
func TestTheNpxShimAsksForAnAssetTheReleaseActuallyBuilds(t *testing.T) {
	root := repoRoot(t)

	nameTemplate := goreleaserArchiveTemplate(t, root)
	goos, goarch := npxPlatformTables(t, root)

	// Every platform the shim will ask for. If the shim learns a new one and
	// the build does not cover it, that is a real gap and this is where it
	// shows up.
	for nodePlatform, os := range goos {
		for nodeArch, arch := range goarch {
			ext := "tar.gz"
			if nodePlatform == "win32" {
				ext = "zip"
			}
			wanted := "nanobots_0.1.0_" + os + "_" + arch + "." + ext

			built := render(t, nameTemplate, map[string]any{
				"ProjectName": "nanobots",
				"Version":     "0.1.0",
				"Os":          goPlatform(nodePlatform),
				"Arch":        goArch(nodeArch),
			}) + "." + ext

			if built != wanted {
				t.Errorf("%s/%s: the shim downloads %q, the release publishes %q",
					nodePlatform, nodeArch, wanted, built)
			}
		}
	}
}

// goreleaserArchiveTemplate pulls the archive name_template out of the real
// config, rather than a copy of it living in this test.
func goreleaserArchiveTemplate(t *testing.T, root string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, ".goreleaser.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	// The template is a folded YAML scalar: `name_template: >-` followed by
	// indented lines until the next key at the same level.
	lines := strings.Split(string(raw), "\n")
	var out []string
	collecting := false
	for _, line := range lines {
		if strings.Contains(line, "name_template: >-") {
			collecting = true
			continue
		}
		if collecting {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" || !strings.HasPrefix(trimmed, "{{") {
				break
			}
			out = append(out, trimmed)
		}
	}
	if len(out) == 0 {
		t.Fatal(".goreleaser.yaml has no folded archive name_template — if it went back to " +
			"the default, npm/bin/nanobots.js needs to change with it")
	}
	// A folded scalar joins its lines with no separator once the {{- }}
	// trim markers are honoured, which is exactly what the Go template
	// engine does with them.
	return strings.Join(out, "\n")
}

// npxPlatformTables reads the shim's own process.platform and process.arch
// maps, so the test cannot drift from the file it is checking.
func npxPlatformTables(t *testing.T, root string) (goos, goarch map[string]string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, "npm", "bin", "nanobots.js"))
	if err != nil {
		t.Fatal(err)
	}
	goos = jsMap(t, string(raw), "GOOS")
	goarch = jsMap(t, string(raw), "GOARCH")
	if len(goos) == 0 || len(goarch) == 0 {
		t.Fatal("could not read the GOOS/GOARCH tables out of npm/bin/nanobots.js")
	}
	return goos, goarch
}

var jsPair = regexp.MustCompile(`(\w+):\s*"([^"]+)"`)

func jsMap(t *testing.T, src, name string) map[string]string {
	t.Helper()
	i := strings.Index(src, "const "+name+" = {")
	if i < 0 {
		return nil
	}
	j := strings.Index(src[i:], "}")
	if j < 0 {
		return nil
	}
	out := map[string]string{}
	for _, m := range jsPair.FindAllStringSubmatch(src[i:i+j], -1) {
		out[m[1]] = m[2]
	}
	return out
}

// goPlatform and goArch translate Node's names to Go's, which is the
// translation the shim performs in reverse.
func goPlatform(nodePlatform string) string {
	switch nodePlatform {
	case "win32":
		return "windows"
	default:
		return nodePlatform
	}
}

func goArch(nodeArch string) string {
	switch nodeArch {
	case "x64":
		return "amd64"
	default:
		return nodeArch
	}
}

func render(t *testing.T, text string, data map[string]any) string {
	t.Helper()
	tmpl, err := template.New("name").Funcs(template.FuncMap{
		// GoReleaser's own `title`, which is where Darwin gets its capital.
		"title": func(s string) string {
			if s == "" {
				return s
			}
			return strings.ToUpper(s[:1]) + s[1:]
		},
	}).Parse(text)
	if err != nil {
		t.Fatalf("parse %q: %v", text, err)
	}
	var b strings.Builder
	if err := tmpl.Execute(&b, data); err != nil {
		t.Fatalf("execute: %v", err)
	}
	return b.String()
}
