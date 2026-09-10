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

func (s *Server) handleListBots(w http.ResponseWriter, r *http.Request) {
	entries, err := os.ReadDir(s.BotsDir)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
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
		bots = append(bots, BotSummary{
			ID: e.Name(), Name: nb.Metadata.Name, Version: nb.Metadata.Version,
			Description: nb.Metadata.Description, Tags: nb.Metadata.Tags,
			Harness: nb.Spec.Harness.Type, Services: nb.Spec.Services,
			Inputs: nb.Spec.Ports.Inputs, Outputs: nb.Spec.Ports.Outputs,
			Guardrails: nb.Spec.Guardrails,
		})
	}
	writeJSON(w, http.StatusOK, bots)
}
