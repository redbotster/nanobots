package api

import (
	"net/http"
	"os"
	"path/filepath"

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
		Guardrails: nb.Spec.Guardrails,
	}
}

func (s *Server) handleListBots(w http.ResponseWriter, r *http.Request) {
	bots, err := s.listBotSummaries()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, bots)
}
