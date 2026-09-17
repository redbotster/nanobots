package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"

	"github.com/redbotster/nanobots/internal/schema"
)

// BotSummary is what the WebUI's bot library needs — the full Nanobot minus
// anything that's really just a file path (prompts, templates), which the
// UI has no use for directly.
type BotSummary struct {
	ID          string              `json:"id"`
	Name        string              `json:"name"`
	Version     string              `json:"version"`
	Description string              `json:"description"`
	Tags        []string            `json:"tags"`
	Harness     string              `json:"harness"`
	Services    []schema.Service    `json:"services"`
	Inputs      []schema.InputPort  `json:"inputs"`
	Outputs     []schema.OutputPort `json:"outputs"`
	Guardrails  schema.Guardrails   `json:"guardrails"`
	// OutputFields lists the fields inside each json output port that
	// declares a schema, so anything choosing a snap knows what it can
	// drill into. See outputFieldsOf.
	OutputFields map[string][]string `json:"output_fields,omitempty"`
}

// listBotSummaries scans s.BotsDir the same way for every caller — the
// bot library endpoint and the AI composer's catalog prompt both need
// exactly this, and must never drift apart.
func (s *Server) listBotSummaries() ([]BotSummary, error) {
	entries, err := os.ReadDir(s.BotsDir)
	if err != nil {
		return nil, err
	}
	var bots []BotSummary
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		nbPath := filepath.Join(s.BotsDir, e.Name(), "nanobot.yaml")
		nb, err := schema.LoadNanobot(nbPath)
		if err != nil {
			continue // not every dir under bots/ need be a bot
		}
		bots = append(bots, botSummaryOf(nb, e.Name()))
	}
	return nonNil(bots), nil
}

// botSummaryOf is the one place a BotSummary is built, so a handler that
// returns a single freshly-edited bot can't drift from the list endpoint.
func botSummaryOf(nb *schema.Nanobot, id string) BotSummary {
	return BotSummary{
		ID: id, Name: nb.Metadata.Name, Version: nb.Metadata.Version,
		Description: nb.Metadata.Description, Tags: nonNil(nb.Metadata.Tags),
		Harness: nb.Spec.Harness.Type, Services: nonNil(nb.Spec.Services),
		Inputs: nonNil(nb.Spec.Ports.Inputs), Outputs: nonNil(nb.Spec.Ports.Outputs),
		Guardrails: nb.Spec.Guardrails, OutputFields: outputFieldsOf(nb),
	}
}

// outputFieldsOf lists the fields inside each json output port that
// declares a schema.
//
// A snap can drill into one — "chaser.drafted.*.subject" — and the planner
// resolves that against the schema file. Anything choosing a snap therefore
// needs to know the fields exist: the AI composer invented a `.summary`
// field on a port that has none, for the simple reason that the catalog it
// was shown listed types and not fields. The visual builder wants the same
// list for the same reason.
func outputFieldsOf(nb *schema.Nanobot) map[string][]string {
	out := map[string][]string{}
	for _, p := range nb.Spec.Ports.Outputs {
		if p.Schema == "" || nb.SourcePath == "" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(nb.SourcePath, filepath.Clean(p.Schema)))
		if err != nil {
			continue
		}
		var doc struct {
			Properties map[string]json.RawMessage `json:"properties"`
			// A list<json> port may point at a schema describing one item
			// directly (this catalog's convention) or at an array schema.
			Items struct {
				Properties map[string]json.RawMessage `json:"properties"`
			} `json:"items"`
		}
		if err := json.Unmarshal(raw, &doc); err != nil {
			continue
		}
		props := doc.Properties
		if len(props) == 0 {
			props = doc.Items.Properties
		}
		if len(props) == 0 {
			continue
		}
		fields := make([]string, 0, len(props))
		for name := range props {
			fields = append(fields, name)
		}
		sort.Strings(fields)
		out[p.Name] = fields
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// The bot catalog is 30KB and changes only when someone flips a service to
// live or edits a bot's instructions — so almost every request for it is
// for bytes the caller already has. Five surfaces fetch it on mount (the
// bot library, Settings, the Team page, a swarm view, the builder), which
// made it the largest consumer left once the run list was behind a
// conditional GET: 61KB of a 336KB five-page browse, measured in a browser.
// The client holds the tag — see web/src/lib/revalidatingList.ts for why
// that is its job rather than the browser's, and for why a client-side copy
// of this list needs no invalidating when a bot changes.
func (s *Server) handleListBots(w http.ResponseWriter, r *http.Request) {
	bots, err := s.listBotSummaries()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSONCached(w, r, http.StatusOK, bots)
}
