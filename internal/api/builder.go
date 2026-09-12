// The visual swarm builder's backend: validate a swarm as it's being built
// (before it's ever saved to a YAML file), save it, and load an existing
// swarm's structured data back out for editing. See web/src/pages/BuilderPage.tsx.
package api

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/redbotster/nanobots/internal/planner"
	"github.com/redbotster/nanobots/internal/schema"
	"gopkg.in/yaml.v3"
)

// builderBotRef/builderSnap are the wire shape for one bot instance and one
// connection in a swarm draft — a small, JSON-friendly subset of
// schema.BotRef/schema.Snap (no `path:` local-bot support here; the builder
// only places bots from the catalog, via `use:`).
type builderBotRef struct {
	ID     string         `json:"id"`
	Use    string         `json:"use"`
	Inputs map[string]any `json:"inputs,omitempty"`
}

type builderSnap struct {
	From string `json:"from"`
	To   string `json:"to"`
	// Join collapses a fanned-out list into one value (docs/fan-out.md).
	//
	// Carried through the builder even though nothing in the UI sets it
	// yet, because the builder round-trips a swarm on every save: a field
	// it doesn't know about is a field it deletes. That is how "Save
	// changes" once removed a swarm's cron trigger and its guardrails, and
	// dropping a join would silently turn get-paid back into chasing one
	// invoice with no warning and no undo.
	Join string `json:"join,omitempty"`
}

func (b builderBotRef) toSchema() schema.BotRef {
	return schema.BotRef{ID: b.ID, Use: b.Use, Inputs: b.Inputs}
}

func (s builderSnap) toSchema() schema.Snap {
	return schema.Snap{From: s.From, To: s.To, Join: s.Join}
}

func draftToNanoswarm(name, description, owner string, bots []builderBotRef, snaps []builderSnap) *schema.Nanoswarm {
	sw := &schema.Nanoswarm{
		APIVersion: "nanobots.dev/v1alpha1",
		Kind:       "Nanoswarm",
		Metadata:   schema.Metadata{Name: name, Description: description, Owner: owner},
		Spec: schema.NanoswarmSpec{
			Trigger: schema.Trigger{Type: "manual"},
			Deploy:  schema.Deploy{Target: "local"},
		},
	}
	for _, b := range bots {
		sw.Spec.Bots = append(sw.Spec.Bots, b.toSchema())
	}
	for _, s := range snaps {
		sw.Spec.Snaps = append(sw.Spec.Snaps, s.toSchema())
	}
	return sw
}

// buildPlanResponse shapes a PlanResult (or a Resolve-time error, when
// result is nil) into the same JSON handlePlan already returns — the
// builder's live validation and `nanobots plan`'s HTTP form share one
// response contract.
func buildPlanResponse(swarmName string, result *planner.PlanResult, resolveErr error) planResponse {
	if resolveErr != nil {
		return planResponse{Swarm: swarmName, Error: resolveErr.Error(), Bots: []botInstanceJSON{}, Snaps: []snapCheckJSON{}}
	}
	resp := planResponse{Swarm: result.Resolved.Swarm.Metadata.Name, OK: result.OK()}
	for instanceID, rb := range result.Resolved.Bots {
		botID, _, _ := strings.Cut(rb.Ref.Use, "@")
		if botID == "" {
			botID = rb.Ref.Path
		}
		resp.Bots = append(resp.Bots, botInstanceJSON{
			InstanceID: instanceID, BotID: botID,
			Name: rb.Nanobot.Metadata.Name, Version: rb.Nanobot.Metadata.Version,
		})
	}
	if result.DAGErr != nil {
		resp.Error = result.DAGErr.Error()
	} else if order, err := result.DAG.TopoSort(); err == nil {
		resp.Order = order
	}
	for _, u := range result.Unfed {
		resp.Unfed = append(resp.Unfed, unfedInputJSON{Bot: u.BotID, Port: u.Port, Reason: u.Why})
	}
	for _, c := range result.Snaps {
		sc := snapCheckJSON{From: c.Snap.From, To: c.Snap.To, OK: c.OK, Join: c.Snap.Join}
		if c.Joined() {
			sc.RawFrom = c.RawFromType.String()
		}
		if c.OK {
			sc.FromType, sc.ToType = c.FromType.String(), c.ToType.String()
		} else {
			sc.Error = c.Err.Error()
		}
		resp.Snaps = append(resp.Snaps, sc)
	}
	resp.Bots = nonNil(resp.Bots)
	for _, u := range result.Unfed {
		resp.Unfed = append(resp.Unfed, unfedInputJSON{Bot: u.BotID, Port: u.Port, Reason: u.Why})
	}
	resp.Snaps = nonNil(resp.Snaps)
	return resp
}

type validateSwarmRequest struct {
	Bots  []builderBotRef `json:"bots"`
	Snaps []builderSnap   `json:"snaps"`
}

// handleValidateSwarm type-checks a swarm draft that may not even be
// structurally saveable yet (no bots, dangling snaps mid-edit) — always
// responds 200 with an ok:false/error explaining why, the same way a single
// bad snap already shows up as a non-fatal per-snap error. Only a malformed
// request body is a hard 400.
func (s *Server) handleValidateSwarm(w http.ResponseWriter, r *http.Request) {
	var req validateSwarmRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	sw := draftToNanoswarm("draft", "", "", req.Bots, req.Snaps)
	result, err := planner.PlanSwarm(sw, s.BotsDir)
	writeJSON(w, http.StatusOK, buildPlanResponse("draft", result, err))
}

type saveSwarmRequest struct {
	Path        string          `json:"path,omitempty"` // set to overwrite an existing swarm; empty creates a new one
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Owner       string          `json:"owner,omitempty"`
	Bots        []builderBotRef `json:"bots"`
	Snaps       []builderSnap   `json:"snaps"`
}

type saveSwarmResponse struct {
	Path        string       `json:"path"`
	Name        string       `json:"name"`
	Description string       `json:"description"`
	Plan        planResponse `json:"plan"`
}

var slugNonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(name string) string {
	s := slugNonAlnum.ReplaceAllString(strings.ToLower(name), "-")
	s = strings.Trim(s, "-")
	if s == "" {
		s = "swarm"
	}
	return s
}

// handleSaveSwarm writes a swarm draft to examples/swarms/<slug>.yaml (or
// overwrites req.Path when editing an existing one). It rejects a
// structurally broken draft (duplicate bot ids, a bot ref the catalog
// doesn't have) — planner.Resolve's own error — but happily saves a draft
// whose snaps don't all type-check yet, so a work in progress is never
// blocked from being saved and returned to later; the response's `plan`
// field tells the caller which is which.
func (s *Server) handleSaveSwarm(w http.ResponseWriter, r *http.Request) {
	var req saveSwarmRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("name is required"))
		return
	}
	if len(req.Bots) == 0 {
		writeError(w, http.StatusBadRequest, fmt.Errorf("add at least one bot before saving"))
		return
	}

	sw := draftToNanoswarm(req.Name, req.Description, req.Owner, req.Bots, req.Snaps)
	result, resolveErr := planner.PlanSwarm(sw, s.BotsDir)
	if resolveErr != nil {
		writeError(w, http.StatusBadRequest, resolveErr)
		return
	}

	dir := s.swarmsDir()
	var path string
	if req.Path != "" {
		abs, err := swarmPathFromRequest(dir, req.Path)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		path = abs
	} else {
		path = uniqueSwarmPath(dir, slugify(req.Name))
	}

	// Overwriting an existing swarm merges into the file rather than
	// replacing it: the builder only models name, description, bots and
	// snaps, and a swarm file holds a trigger, vars, guardrail defaults, a
	// deploy target, an owner and comments besides. See swarmmerge.go for
	// what pressing "Save changes" used to destroy.
	var raw []byte
	var err error
	if existing, readErr := os.ReadFile(path); readErr == nil {
		raw, err = mergeIntoExistingSwarm(existing, req.Name, req.Description, req.Bots, req.Snaps)
		if err != nil {
			writeError(w, http.StatusInternalServerError,
				fmt.Errorf("could not update %s without losing the rest of the file: %w", filepath.Base(path), err))
			return
		}
	} else {
		raw, err = yaml.Marshal(sw)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	relPath, err := filepath.Rel(filepath.Dir(s.BotsDir), path)
	if err != nil {
		relPath = path
	}
	writeJSON(w, http.StatusOK, saveSwarmResponse{
		Path: relPath, Name: req.Name, Description: req.Description,
		Plan: buildPlanResponse(req.Name, result, nil),
	})
}

// swarmPathFromRequest resolves a caller-supplied swarm path against dir,
// keeping only its final path segment before joining — unlike
// handleSwarmYAML's raw os.ReadFile(path) (which reads whatever absolute or
// relative path it's given, so it needs its own separate ".." substring
// guard), this always joins against the known-safe dir, so no directory
// component the caller supplies, ".." or otherwise, can ever take effect;
// filepath.Base strips it before dir even sees it.
func swarmPathFromRequest(dir, relPath string) (string, error) {
	base := filepath.Base(relPath)
	abs := filepath.Join(dir, base)
	if _, err := os.Stat(abs); err != nil {
		return "", fmt.Errorf("swarm %q not found: %w", relPath, err)
	}
	return abs, nil
}

// uniqueSwarmPath appends -2, -3, ... to slug until dir/<slug>.yaml doesn't
// already exist, so saving two swarms with the same name never silently
// clobbers the first one.
func uniqueSwarmPath(dir, slug string) string {
	path := filepath.Join(dir, slug+".yaml")
	for i := 2; ; i++ {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return path
		}
		path = filepath.Join(dir, slug+"-"+strconv.Itoa(i)+".yaml")
	}
}

type swarmFullResponse struct {
	Path        string          `json:"path"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Owner       string          `json:"owner"`
	Bots        []builderBotRef `json:"bots"`
	Snaps       []builderSnap   `json:"snaps"`
}

// handleGetSwarmFull returns an existing swarm's structured bots/snaps —
// what the builder needs to load a saved swarm back in for editing, as
// opposed to handleSwarmYAML's raw text (for the read-only YAML drawer) or
// handlePlan's type-checked-but-lossy summary.
func (s *Server) handleGetSwarmFull(w http.ResponseWriter, r *http.Request) {
	relPath := r.URL.Query().Get("path")
	if relPath == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("?path= is required"))
		return
	}
	abs, err := swarmPathFromRequest(s.swarmsDir(), relPath)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	sw, err := schema.LoadNanoswarm(abs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	resp := swarmFullResponse{
		Path: relPath, Name: sw.Metadata.Name, Description: sw.Metadata.Description, Owner: sw.Metadata.Owner,
	}
	for _, b := range sw.Spec.Bots {
		resp.Bots = append(resp.Bots, builderBotRef{ID: b.ID, Use: b.Use, Inputs: b.Inputs})
	}
	for _, sn := range sw.Spec.Snaps {
		resp.Snaps = append(resp.Snaps, builderSnap{From: sn.From, To: sn.To, Join: sn.Join})
	}
	resp.Bots = nonNil(resp.Bots)
	resp.Snaps = nonNil(resp.Snaps)
	writeJSON(w, http.StatusOK, resp)
}
