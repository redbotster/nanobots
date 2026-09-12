package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Pinning a real run as a bot's test data.
//
// Every bot here ships fixtures so `nanobots conform` and `go test` can run
// it offline — and those fixtures are hand-written, which makes them
// somebody's guess at what a model or an API returns. Guesses drift: a
// fixture written before a prompt changed still passes while the real bot
// has been broken for weeks.
//
// A finished run holds the real thing (see step.RecordingDeps). This is
// where a person decides to keep it.
//
// It is a deliberate two-step: GET says exactly what would be written and
// what it would replace, POST does it. Writing into bots/ is a real change
// to the repo — it belongs in a commit, and a button that silently
// rewrote test data would be the worst possible way to find that out.

type fixturePreview struct {
	File string `json:"file"`
	// Status is "new" or "replaces". A fixture that would overwrite
	// existing test data is the one worth looking at twice.
	Status string `json:"status"`
	// Content is the JSON that would be written, pretty-printed, so the UI
	// can show it rather than describe it.
	Content string `json:"content"`
	// Current is what is on disk today, when this replaces something.
	Current string `json:"current,omitempty"`
}

type fixturesResponse struct {
	Bots map[string][]fixturePreview `json:"bots"`
	// Error explains why there is nothing to offer, when that isn't
	// obvious — a demo run records its own fixtures back at itself, which
	// is true but useless.
	Error string `json:"error,omitempty"`
}

func (s *Server) handleRunFixtures(w http.ResponseWriter, r *http.Request) {
	run, err := s.Runs.Get(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	resp := fixturesResponse{Bots: map[string][]fixturePreview{}}
	for botID, fixtures := range run.Captured() {
		dir, err := s.fixturesDirFor(botID)
		if err != nil {
			continue
		}
		var previews []fixturePreview
		for name, value := range fixtures {
			content, err := marshalFixture(value)
			if err != nil {
				continue
			}
			p := fixturePreview{File: name, Status: "new", Content: content}
			if existing, err := os.ReadFile(filepath.Join(dir, name)); err == nil {
				p.Current = string(existing)
				if strings.TrimSpace(p.Current) == strings.TrimSpace(content) {
					// Identical to what's already committed: nothing to
					// decide, and listing it as a change would be noise.
					continue
				}
				p.Status = "replaces"
			}
			previews = append(previews, p)
		}
		sort.Slice(previews, func(i, j int) bool { return previews[i].File < previews[j].File })
		if len(previews) > 0 {
			resp.Bots[botID] = previews
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

type pinFixturesRequest struct {
	// Bot limits the write to one bot. Empty writes every bot's.
	Bot string `json:"bot,omitempty"`
	// Files limits the write to specific filenames. Empty writes all of
	// that bot's. Present so a person can keep the model's answer and
	// reject a service response that happened to be empty today.
	Files []string `json:"files,omitempty"`
}

type pinFixturesResponse struct {
	Written []string `json:"written"`
}

func (s *Server) handlePinFixtures(w http.ResponseWriter, r *http.Request) {
	run, err := s.Runs.Get(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	var req pinFixturesRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	wanted := map[string]bool{}
	for _, f := range req.Files {
		wanted[f] = true
	}

	var written []string
	for botID, fixtures := range run.Captured() {
		if req.Bot != "" && botID != req.Bot {
			continue
		}
		dir, err := s.fixturesDirFor(botID)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		for name, value := range fixtures {
			if len(wanted) > 0 && !wanted[name] {
				continue
			}
			if !safeFixtureName(name) {
				writeError(w, http.StatusBadRequest, fmt.Errorf("refusing to write %q", name))
				return
			}
			content, err := marshalFixture(value)
			if err != nil {
				continue
			}
			if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
				writeError(w, http.StatusInternalServerError, err)
				return
			}
			written = append(written, botID+"/"+name)
		}
	}
	sort.Strings(written)
	writeJSON(w, http.StatusOK, pinFixturesResponse{Written: written})
}

// fixturesDirFor resolves bots/<id>/fixtures, refusing anything that would
// leave BotsDir. The bot id comes from a run, not from a request, but this
// endpoint writes files — the check costs nothing and the alternative is
// trusting a path all the way to os.WriteFile.
func (s *Server) fixturesDirFor(botID string) (string, error) {
	if botID == "" || strings.ContainsAny(botID, `/\`) || strings.Contains(botID, "..") {
		return "", fmt.Errorf("invalid bot id %q", botID)
	}
	root, err := filepath.Abs(s.BotsDir)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(root, botID, "fixtures")
	if !strings.HasPrefix(dir, root+string(os.PathSeparator)) {
		return "", fmt.Errorf("invalid bot id %q", botID)
	}
	if _, err := os.Stat(filepath.Join(root, botID, "nanobot.yaml")); err != nil {
		return "", fmt.Errorf("no bot %q in the catalog", botID)
	}
	return dir, nil
}

// safeFixtureName allows only the shapes fixtureName produces.
func safeFixtureName(name string) bool {
	if !strings.HasSuffix(name, ".json") || strings.ContainsAny(name, `/\`) || strings.Contains(name, "..") {
		return false
	}
	return name != ".json"
}

// marshalFixture writes the same shape a person would: two-space indented,
// newline-terminated, so pinning a fixture produces a diff someone can read
// rather than one line of minified JSON.
func marshalFixture(v any) (string, error) {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", err
	}
	return string(raw) + "\n", nil
}
