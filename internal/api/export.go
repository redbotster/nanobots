package api

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/redbotster/nanobots/internal/schema"
	"github.com/redbotster/nanobots/internal/share"
)

// Exporting a swarm from the app rather than from a terminal.
//
// internal/share has been able to bundle one since it was written, reachable
// only as `nanobots export -f examples/swarms/x.yaml`. That is backwards:
// the person most likely to want to hand a swarm to someone else is the one
// who just built it in the visual builder, and they are not in a terminal.
//
// The response is the bundle itself as text/yaml with a filename, so a
// browser saves it. No new format, no second code path — the same
// share.Export the CLI calls, so a bundle made here and one made there are
// the same file.

// handleExportSwarm returns a shareable bundle for one swarm.
func (s *Server) handleExportSwarm(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("export: ?path= is required"))
		return
	}
	full, err := s.resolveSwarmPath(path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	raw, err := os.ReadFile(full)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	sw, err := schema.LoadNanoswarm(full)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	bundle, err := share.Export(raw, sw, func(use string) (*schema.Nanobot, error) {
		id, _, _ := strings.Cut(use, "@")
		return schema.LoadNanobot(filepath.Join(s.BotsDir, id, "nanobot.yaml"))
	})
	if err != nil {
		// A `path:` bot can't be shared, and share.Export says why. That is
		// a 409 rather than a 500: nothing is broken, this swarm just isn't
		// shareable as written.
		writeError(w, http.StatusConflict, err)
		return
	}
	body, err := bundle.Marshal()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	name := sw.Metadata.Name
	if name == "" {
		name = strings.TrimSuffix(filepath.Base(full), ".yaml")
	}
	w.Header().Set("Content-Type", "application/x-yaml; charset=utf-8")
	// Content-Disposition so the browser saves it under a name that says
	// what it is — a bundle, not the swarm file it contains.
	w.Header().Set("Content-Disposition",
		fmt.Sprintf("attachment; filename=%q", name+".nanoswarm.yaml"))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

// resolveSwarmPath turns a path from a query string into an absolute path
// inside the swarms directory, refusing anything that points outside it.
//
// The other swarm handlers take ?path= straight from the client, which is
// safe enough while every one of them only reads YAML the daemon already
// serves. This one hands back a file the browser saves, so it gets the
// check — and it is cheap enough that the rest should grow it too.
func (s *Server) resolveSwarmPath(path string) (string, error) {
	dir, err := filepath.Abs(s.swarmsDir())
	if err != nil {
		return "", err
	}
	// A relative path is resolved against the process's own working
	// directory first — that is what "examples/swarms/x.yaml", the form
	// /api/swarms hands out, means — and then checked against the swarms
	// directory regardless of which way it was written.
	full, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(full, dir+string(filepath.Separator)) {
		return "", fmt.Errorf("%s is not a swarm in %s", path, dir)
	}
	return full, nil
}

// ImportRequest carries a pasted or dropped bundle.
type ImportRequest struct {
	Bundle string `json:"bundle"`
	// Confirm is the second step. The first call reports what the bundle
	// will do; only a call with confirm:true writes anything.
	Confirm bool `json:"confirm"`
}

// ImportResult describes a bundle, and — once confirmed — where it landed.
type ImportResult struct {
	Name     string   `json:"name"`
	Requires []string `json:"requires,omitempty"`
	Connects []string `json:"connects,omitempty"`
	// Acts is the one thing someone must read before running a stranger's
	// automation, so it is the reason this is two steps and not one.
	Acts []string `json:"acts,omitempty"`
	// Missing is the catalog bots this machine does not have. Non-empty
	// means nothing can be written, confirmed or not.
	Missing  []string `json:"missing,omitempty"`
	Imported bool     `json:"imported"`
	Path     string   `json:"path,omitempty"`
}

// handleImportSwarm previews a bundle, then writes it on a second call.
//
// Two steps on purpose. A swarm is executable — `get-paid` emails your
// customers — and the bundle format exists specifically so that what it
// will do to the outside world is visible before it runs. A one-click
// import that saves first and shows the warning after has thrown that away.
func (s *Server) handleImportSwarm(w http.ResponseWriter, r *http.Request) {
	var req ImportRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	bundle, err := share.Parse([]byte(req.Bundle))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	out := ImportResult{
		Name:     bundle.Name,
		Requires: bundle.Requires,
		Connects: bundle.Connects,
		Acts:     bundle.Acts,
	}
	out.Missing = bundle.Missing(func(use string) bool {
		id, _, _ := strings.Cut(use, "@")
		nb, err := schema.LoadNanobot(filepath.Join(s.BotsDir, id, "nanobot.yaml"))
		return err == nil && nb != nil
	})
	// Checked before writing anything, both times: a swarm referencing a
	// bot you don't have is not importable, and finding that out at run
	// time — after it is saved and looks legitimate — is the worse order.
	if len(out.Missing) > 0 || !req.Confirm {
		writeJSON(w, http.StatusOK, out)
		return
	}

	if strings.TrimSpace(bundle.Name) == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("this bundle has no swarm name"))
		return
	}
	// The package's own slugify, so a bundle imported here lands at the
	// same filename handleSaveSwarm would have chosen for the same name.
	dest := filepath.Join(s.swarmsDir(), slugify(bundle.Name)+".yaml")
	if _, err := os.Stat(dest); err == nil {
		writeError(w, http.StatusConflict, fmt.Errorf(
			"%s already exists — rename the swarm in the bundle, or move the existing file", dest))
		return
	}
	if err := os.MkdirAll(s.swarmsDir(), 0o755); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if err := os.WriteFile(dest, []byte(bundle.Swarm), 0o644); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	// See handleSaveSwarm's identical call: a freshly imported swarm isn't
	// in the cached inspect map yet, and a stale-cold-cache read of a
	// missing key defaults to 0/0 — the "everything is broken" bug, not a
	// harmless delay.
	s.swarmInspectCache.invalidate()
	out.Imported, out.Path = true, dest
	writeJSON(w, http.StatusOK, out)
}
