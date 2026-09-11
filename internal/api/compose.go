// The "head nanobot" — a natural-language swarm composer. A human describes
// what they want automated ("Help me automate a daily email recap and list
// it by priority"); this asks Shroud to snap together a draft swarm from
// the real bot catalog, validates it through the same planner a real run
// uses, and hands it back for the WebUI's visual builder to show for
// review — this never saves or runs anything on its own. See
// web/src/pages/SwarmsPage.tsx's compose box and BuilderPage's
// hydrateFrom, which this response feeds directly.
package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/redbotster/nanobots/internal/oneclaw"
	"github.com/redbotster/nanobots/internal/planner"
	"github.com/redbotster/nanobots/internal/schema"
)

const composeAgentName = "nanobots-composer"

type composeRequest struct {
	Message string `json:"message"`
}

// composeGapPayload is what the model returns instead of a draft when it
// determines no combination of the real catalog can satisfy the request —
// see composePrompt's second legal output shape. This is what a caller
// (the WebUI's gap panel, eventually the foundry) uses to brief a coding
// agent on exactly what's missing, rather than guessing from a rejected
// draft.
type composeGapPayload struct {
	MissingCapability string              `json:"missing_capability"`
	SuggestedInputs   []schema.InputPort  `json:"suggested_inputs,omitempty"`
	SuggestedOutputs  []schema.OutputPort `json:"suggested_outputs,omitempty"`
}

// composeResponse carries exactly one of Draft or Gap, never both — a
// pointer/omitempty pair rather than a single struct, since a gap has no
// meaningful zero-value draft to fall back on.
type composeResponse struct {
	Draft *saveSwarmRequest  `json:"draft,omitempty"`
	Plan  *planResponse      `json:"plan,omitempty"`
	Gap   *composeGapPayload `json:"gap,omitempty"`
}

func (s *Server) handleCompose(w http.ResponseWriter, r *http.Request) {
	if s.OneClaw == nil || !s.OneClaw.Configured() {
		writeError(w, http.StatusBadRequest, fmt.Errorf("1Claw isn't configured yet — add ONECLAW_API_KEY first"))
		return
	}
	var req composeRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(req.Message) == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("message is required"))
		return
	}

	bots, err := s.listBotSummaries()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if len(bots) == 0 {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("no bots found in the catalog to compose from"))
		return
	}

	agentID, agentAPIKey, err := s.OneClaw.EnsureAgent(s.Orchestrator.AgentStateDir, composeAgentName, oneclaw.CreateAgentRequest{
		ShroudEnabled: true,
		ShroudConfig:  &oneclaw.ShroudConfig{PIIPolicy: "redact", EnableSecretRedaction: true},
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("ensure composer agent: %w", err))
		return
	}
	shroud := oneclaw.NewShroudClient(agentID, agentAPIKey)

	raw, err := shroud.Chat("anthropic", "claude-sonnet-4-6", composePrompt(bots, req.Message), 3000)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("compose: %w", err))
		return
	}
	draft, gap, err := parseComposeResponse(raw)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("compose produced an unusable response: %w", err))
		return
	}
	if gap != nil {
		writeJSON(w, http.StatusOK, composeResponse{Gap: gap})
		return
	}

	sw := draftToNanoswarm(draft.Name, draft.Description, "", draft.Bots, draft.Snaps)
	result, resolveErr := planner.PlanSwarm(sw, s.BotsDir)
	plan := buildPlanResponse(draft.Name, result, resolveErr)
	writeJSON(w, http.StatusOK, composeResponse{Draft: draft, Plan: &plan})
}

// composePrompt enumerates the real, live catalog — id, name, description,
// tags, and every port with its type — so the model can only propose bots
// and connections that actually exist. It never sees anything about the
// user's own data; only the catalog's own declared shapes.
func composePrompt(bots []BotSummary, message string) string {
	var b strings.Builder
	b.WriteString("You are the Nanobots composer: you turn a plain-English automation request into a nanoswarm — a small graph of bots snapped together by typed ports.\n\n")
	b.WriteString("Here is the full bot catalog available to you. You may ONLY use these bots, by their exact id and version, and ONLY connect a snap between ports whose types genuinely match:\n\n")
	for _, bot := range bots {
		fmt.Fprintf(&b, "- %s@%s (%s): %s\n", bot.ID, bot.Version, bot.Name, bot.Description)
		if len(bot.Inputs) > 0 {
			b.WriteString("  inputs: ")
			parts := make([]string, len(bot.Inputs))
			for i, p := range bot.Inputs {
				parts[i] = fmt.Sprintf("%s:%s", p.Name, p.Type)
			}
			b.WriteString(strings.Join(parts, ", "))
			b.WriteString("\n")
		}
		if len(bot.Outputs) > 0 {
			b.WriteString("  outputs: ")
			parts := make([]string, len(bot.Outputs))
			for i, p := range bot.Outputs {
				parts[i] = fmt.Sprintf("%s:%s", p.Name, p.Type)
			}
			b.WriteString(strings.Join(parts, ", "))
			b.WriteString("\n")
		}
	}
	b.WriteString(`
The user's request: "` + message + `"

Produce **only** JSON in this shape:

{
  "name": "a short, human-readable swarm name",
  "description": "one sentence describing what it does",
  "bots": [
    { "id": "a short instance id you choose, e.g. 'triage'", "use": "<catalog-id>@<version>", "inputs": {} }
  ],
  "snaps": [
    { "from": "<instance-id>.<output-port>", "to": "<instance-id>.<input-port>" }
  ]
}

Rules:
- Pick the smallest set of bots that actually accomplishes the request — usually 1-4.
- Every "use" must be exactly "<catalog-id>@<version>" from the list above, verbatim.
- Every snap's port names and types must genuinely match what's declared above for that bot.
- If a bot's required input isn't fed by a snap, either give it a literal value in that bot's own "inputs" map, or leave it for the human to fill in — never invent a snap from a port that doesn't exist to satisfy it.
- Prefer bots whose job already includes an approval gate (their bot.md/description says so) for anything that sends, posts, pays, or deletes.
- Output raw JSON only, no prose, no markdown fences.

If, and only if, no combination of the catalog above — even with reasonable snaps — can accomplish this request, respond instead with exactly this shape and nothing else:

{
  "gap": true,
  "missing_capability": "one precise sentence describing what capability is missing",
  "suggested_inputs": [{"name": "...", "type": "..."}],
  "suggested_outputs": [{"name": "...", "type": "..."}]
}

Only use this if the catalog genuinely cannot do it — don't reach for it just because the best-fit swarm is a little awkward.
`)
	return b.String()
}

// parseComposeResponse tolerates a model wrapping its JSON in a markdown
// code fence (a common enough real-world response shape that internal/step's
// own ai.generate handling strips it too), then peeks at a "gap" discriminator
// before committing to either full unmarshal — see composeGapPayload's doc
// comment for what a gap response means and who consumes it.
func parseComposeResponse(raw string) (draft *saveSwarmRequest, gap *composeGapPayload, err error) {
	cleaned := strings.TrimSpace(raw)
	cleaned = strings.TrimPrefix(cleaned, "```json")
	cleaned = strings.TrimPrefix(cleaned, "```")
	cleaned = strings.TrimSuffix(cleaned, "```")
	cleaned = strings.TrimSpace(cleaned)

	var peek struct {
		Gap bool `json:"gap"`
	}
	if err := json.Unmarshal([]byte(cleaned), &peek); err != nil {
		return nil, nil, fmt.Errorf("model response was not valid JSON: %w", err)
	}

	if peek.Gap {
		var g composeGapPayload
		if err := json.Unmarshal([]byte(cleaned), &g); err != nil {
			return nil, nil, fmt.Errorf("model's gap response was malformed: %w", err)
		}
		if strings.TrimSpace(g.MissingCapability) == "" {
			return nil, nil, fmt.Errorf("model declared a gap but gave no missing_capability")
		}
		return nil, &g, nil
	}

	var d saveSwarmRequest
	if err := json.Unmarshal([]byte(cleaned), &d); err != nil {
		return nil, nil, fmt.Errorf("model response was not valid JSON: %w", err)
	}
	if d.Name == "" {
		return nil, nil, fmt.Errorf("model response had no swarm name")
	}
	return &d, nil, nil
}
