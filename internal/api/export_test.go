package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/redbotster/nanobots/internal/share"
)

// exportServer points a server at a temp swarms dir and a real bots dir, so
// share.Export resolves catalog bots the same way the CLI does.
func exportServer(t *testing.T, files map[string]string) *Server {
	t.Helper()
	srv := testServer(t)
	dir := t.TempDir()
	srv.SwarmsDir = dir
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name+".yaml"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return srv
}

func get(srv *Server, path string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, r)
	return rec
}

// The bundle the app produces and the bundle the CLI produces have to be
// the same file — two ways to share a swarm that disagree is worse than
// one.
func TestExportServesABundleTheCLIWouldRecognise(t *testing.T) {
	srv := exportServer(t, map[string]string{"intake": webhookSwarm})

	rec := get(srv, "/api/swarms/export?path="+filepath.Join(srv.SwarmsDir, "intake.yaml"))
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, body = %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "yaml") {
		t.Errorf("content-type = %q", ct)
	}
	// Saved under a name that says it is a bundle, not the swarm file
	// inside it.
	if cd := rec.Header().Get("Content-Disposition"); !strings.Contains(cd, "intake.nanoswarm.yaml") {
		t.Errorf("content-disposition = %q", cd)
	}

	b, err := share.Parse(rec.Body.Bytes())
	if err != nil {
		t.Fatalf("the CLI's own parser cannot read what the app exported: %v", err)
	}
	if b.Name != "intake" {
		t.Errorf("name = %q", b.Name)
	}
	// The swarm goes in verbatim, comments and all — that is most of what a
	// reader needs.
	if !strings.Contains(b.Swarm, "kind: Nanoswarm") {
		t.Errorf("bundle does not carry the swarm:\n%s", b.Swarm)
	}
}

// ?path= comes from the client, and this handler is the one that hands back
// a file. A path outside the swarms directory is refused rather than read.
func TestExportRefusesAPathOutsideTheSwarmsDirectory(t *testing.T) {
	srv := exportServer(t, map[string]string{"intake": webhookSwarm})
	outside := filepath.Join(t.TempDir(), "secrets.yaml")
	if err := os.WriteFile(outside, []byte("nope"), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{
		outside,
		filepath.Join(srv.SwarmsDir, "..", filepath.Base(filepath.Dir(outside)), "secrets.yaml"),
		"/etc/passwd",
	} {
		rec := get(srv, "/api/swarms/export?path="+path)
		if rec.Code == http.StatusOK {
			t.Errorf("%s was served; it is not in the swarms directory", path)
		}
	}
}

func TestExportSaysWhichSwarmIsMissing(t *testing.T) {
	srv := exportServer(t, nil)
	rec := get(srv, "/api/swarms/export?path="+filepath.Join(srv.SwarmsDir, "ghost.yaml"))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("code = %d, want 404; body = %s", rec.Code, rec.Body.String())
	}
}

// Import is two steps because a swarm is executable. The first call must
// describe what the thing does and write nothing — a one-click import that
// saves first and warns after has thrown away the reason the bundle format
// carries `acts` at all.
func TestImportPreviewsBeforeItWritesAnything(t *testing.T) {
	srv := exportServer(t, nil)
	bundle := `format: 1
name: borrowed
swarm: |
  apiVersion: nanobots.dev/v1alpha1
  kind: Nanoswarm
  metadata:
    name: borrowed
  spec:
    bots: []
    snaps: []
acts:
  - gmail
`
	rec := postJSON(t, srv, "/api/swarms/import", ImportRequest{Bundle: bundle})
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, body = %s", rec.Code, rec.Body.String())
	}
	var got ImportResult
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Imported {
		t.Error("an unconfirmed import wrote the swarm")
	}
	if len(got.Acts) != 1 || got.Acts[0] != "gmail" {
		t.Errorf("acts = %v, and that is the part the reader must see first", got.Acts)
	}
	if _, err := os.Stat(filepath.Join(srv.SwarmsDir, "borrowed.yaml")); !os.IsNotExist(err) {
		t.Error("the preview call put a file on disk")
	}

	// Confirmed, it lands.
	rec = postJSON(t, srv, "/api/swarms/import", ImportRequest{Bundle: bundle, Confirm: true})
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.Imported {
		t.Fatalf("confirmed import did not write: %s", rec.Body.String())
	}
	saved, err := os.ReadFile(filepath.Join(srv.SwarmsDir, "borrowed.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(saved), "kind: Nanoswarm") {
		t.Errorf("what landed is not the swarm:\n%s", saved)
	}
}

// A bundle naming a bot this machine doesn't have is not importable, and
// saying so before writing is the whole point — a swarm that looks saved
// and fails at run time is the worse order.
func TestImportRefusesWhenABotIsMissing(t *testing.T) {
	srv := exportServer(t, nil)
	bundle := `format: 1
name: needs-more
swarm: |
  apiVersion: nanobots.dev/v1alpha1
  kind: Nanoswarm
  metadata:
    name: needs-more
  spec:
    bots: []
    snaps: []
requires:
  - no-such-bot@9.9.9
`
	rec := postJSON(t, srv, "/api/swarms/import", ImportRequest{Bundle: bundle, Confirm: true})

	var got ImportResult
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Imported {
		t.Error("imported a swarm whose bots are missing")
	}
	if len(got.Missing) != 1 || got.Missing[0] != "no-such-bot@9.9.9" {
		t.Errorf("missing = %v, want the bot named", got.Missing)
	}
	if _, err := os.Stat(filepath.Join(srv.SwarmsDir, "needs-more.yaml")); !os.IsNotExist(err) {
		t.Error("confirm:true wrote the file anyway")
	}
}

// The likeliest mistake is handing import a plain swarm file. Naming that
// beats "format is 0".
func TestImportSaysSoWhenGivenAPlainSwarm(t *testing.T) {
	srv := exportServer(t, nil)
	rec := postJSON(t, srv, "/api/swarms/import", ImportRequest{Bundle: webhookSwarm})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "plain swarm YAML") {
		t.Errorf("unhelpful refusal: %s", rec.Body.String())
	}
}
