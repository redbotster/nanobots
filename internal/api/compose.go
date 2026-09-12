// The "head nanobot" — a natural-language swarm composer. A human describes
// what they want automated ("Help me automate a daily email recap and list
// it by priority"); this asks whichever model this deployment configured
// (see internal/llm) to snap together a draft swarm from the real bot
// catalog, validates it through the same planner a real run
// uses, and hands it back for the WebUI's visual builder to show for
// review — this never saves or runs anything on its own. See
// web/src/pages/SwarmsPage.tsx's compose box and BuilderPage's
// hydrateFrom, which this response feeds directly.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/redbotster/nanobots/internal/llm"
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
	// What the composer needs is a model, which is not the same as needing
	// 1Claw. This used to demand ONECLAW_API_KEY specifically, so someone
	// with a working Gemini key had a working catalog, working bots, and a
	// compose box — the product's primary entry point — that refused to do
	// anything.
	if s.Orchestrator == nil || s.Orchestrator.LLM == nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf(
			"the composer needs a model — set ONECLAW_API_KEY, or one of "+
				"ANTHROPIC_API_KEY / OPENAI_API_KEY / GEMINI_API_KEY (see docs/llm.md)"))
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

	gen, err := s.composerGenerator()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	raw, err := composeChat(r.Context(), gen, composePrompt(bots, req.Message))
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

	draft, plan := s.planWithOneCorrection(r.Context(), bots, req.Message, draft, gen)
	writeJSON(w, http.StatusOK, composeResponse{Draft: draft, Plan: &plan})
}

// planWithOneCorrection type-checks the model's draft and, if the planner
// rejects it, hands the exact failures back for a single retry.
//
// The composer sees every port's declared type, and still gets this wrong:
// asked for "summarise my inbox and post the digest to Slack" it produced
// inbox-triage -> notify with `triage.triaged_count -> notify.message`, a
// json output into a string input. The draft is never auto-saved, so a
// broken one is not dangerous — but it is the first thing a novice sees
// from the product's primary entry point, and "here's a draft, it doesn't
// work, go fix it in the builder" is a bad first impression when the fix is
// mechanical.
//
// One retry, not a loop: the planner is ground truth and cheap, but each
// attempt is a real LLM call the user waits on (~12s). If the second attempt
// is no better, return whichever is closer to working and let the builder
// show the errors, exactly as before — never worse than not trying.
func (s *Server) planWithOneCorrection(
	ctx context.Context, bots []BotSummary, message string, draft *saveSwarmRequest, gen llm.Generator,
) (*saveSwarmRequest, planResponse) {
	plan := s.planDraft(draft)
	if plan.OK {
		return draft, plan
	}

	retryRaw, err := composeChat(ctx, gen, composeRetryPrompt(bots, message, draft, plan))
	if err != nil {
		return draft, plan // the first attempt is still the best we have
	}
	retryDraft, retryGap, err := parseComposeResponse(retryRaw)
	// A gap on the retry is not actionable here: the model already committed
	// to a draft, and switching to "the catalog can't do this" after the
	// fact would discard a draft the human can still fix by hand.
	if err != nil || retryGap != nil || retryDraft == nil {
		return draft, plan
	}
	retryPlan := s.planDraft(retryDraft)
	if !retryPlan.OK && countBadSnaps(retryPlan) >= countBadSnaps(plan) {
		return draft, plan // no better; don't churn the user's draft for nothing
	}
	return retryDraft, retryPlan
}

func (s *Server) planDraft(draft *saveSwarmRequest) planResponse {
	sw := draftToNanoswarm(draft.Name, draft.Description, "", draft.Bots, draft.Snaps)
	result, resolveErr := planner.PlanSwarm(sw, s.BotsDir)
	return buildPlanResponse(draft.Name, result, resolveErr)
}

func countBadSnaps(p planResponse) int {
	n := 0
	if p.Error != "" {
		n++
	}
	for _, sc := range p.Snaps {
		if !sc.OK {
			n++
		}
	}
	return n
}

// composeRetryPrompt asks for a fix to a specific, already-validated
// failure, rather than asking again from scratch. It restates the whole
// catalog (the model has no memory between Shroud calls — each is a
// single-shot completion) plus the draft it produced and exactly which
// snaps the real planner rejected and why.
//
// The planner's own error text is quoted verbatim: "cannot snap
// triage.triaged_count (json) to notify.message (string): types are not
// assignable" names both types and both ports, which is more precise than
// any paraphrase, and it's the same text the human would see in the builder.
func composeRetryPrompt(bots []BotSummary, message string, draft *saveSwarmRequest, plan planResponse) string {
	var b strings.Builder
	b.WriteString(composePrompt(bots, message))
	b.WriteString("\n\n---\n\nYou already answered this request with the following draft, and the real type checker REJECTED it:\n\n")

	if draftJSON, err := json.MarshalIndent(draft, "", "  "); err == nil {
		b.Write(draftJSON)
		b.WriteString("\n\n")
	}
	b.WriteString("Problems the type checker found:\n")
	if plan.Error != "" {
		fmt.Fprintf(&b, "- %s\n", plan.Error)
	}
	for _, sc := range plan.Snaps {
		if !sc.OK {
			fmt.Fprintf(&b, "- snap %s -> %s: %s\n", sc.From, sc.To, sc.Error)
		}
	}
	for _, u := range plan.Unfed {
		fmt.Fprintf(&b, "- bot %q has a required input %q that nothing fills: %s\n", u.Bot, u.Port, u.Reason)
	}
	for _, e := range plan.Invalid {
		fmt.Fprintf(&b, "- %s\n", e)
	}
	b.WriteString(`
Fix these specific problems and return the corrected JSON in the same shape, and nothing else.

- A snap is only legal when the output port's type is assignable to the input port's type. Re-read the catalog types above before connecting anything.
- If no legal snap can carry the data between two bots you wanted to connect, drop that snap. An *optional* input left unconnected is fine — the human fills it in — but a snap that doesn't type-check is not.
- A list feeding a single-item port is not a type error to drop: it is a fan-out. Put ".*" on the from side. A fanned-out bot's list output feeding a single-value port is a "join".
- A *required* input must be filled, by a snap or by an "inputs" value on the bot. A draft that leaves one empty cannot run at all. When no upstream bot produces it, put a sensible literal in "inputs" — an email address, a channel, a label — rather than leaving it out.
- Do not add bots that weren't needed. Do not answer with a gap; you already committed to a draft.
`)
	return b.String()
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
				// The fields, for a json port that declares a schema.
				// Without these the model cannot write a snap that drills
				// into one — "chaser.drafted.*.subject" requires knowing
				// that `drafted` has a subject — so it guesses, and a
				// plausible-sounding field that isn't there fails exactly
				// like a misspelled port. A real composed draft invented
				// `.summary` for precisely this reason.
				if fields := bot.OutputFields[p.Name]; len(fields) > 0 {
					parts[i] += "{" + strings.Join(fields, ",") + "}"
				}
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
    { "id": "a short instance id you choose, e.g. 'triage'", "use": "<catalog-id>@<version>", "inputs": {}, "on_error": "stop" }
  ],
  "snaps": [
    { "from": "<instance-id>.<output-port>", "to": "<instance-id>.<input-port>", "join": "" }
  ]
}

"on_error" and "join" are optional; omit them unless you mean them. Note
where they live: "on_error" is a sibling of "id" and "use", never a key
inside "inputs" — "inputs" holds only that bot's declared input ports.
"join" is a sibling of "from" and "to" on a snap.

Rules:
- Pick the smallest set of bots that actually accomplishes the request — usually 1-4.
- Every "use" must be exactly "<catalog-id>@<version>" from the list above, verbatim.
- Every snap's port names and types must genuinely match what's declared above for that bot. When drilling into a json port with a dot (e.g. "x.items.*.subject"), the field must be one the catalog lists for that port's schema — a plausible-sounding field that isn't there fails the same as a misspelled port.
- Every *required* input must end up filled — by a snap, or by a literal in that bot's own "inputs" map. Leaving one empty produces a swarm that cannot run at all, and the planner will reject it. Never invent a snap from a port that doesn't exist to satisfy one.
- A value in "inputs" is a real, literal value — an email address, a Slack channel, a label. It is NEVER a reference to another bot's port. Writing "draft_id": "chaser.draft_ids[0]" does not read that port; it sends the characters c-h-a-s-e-r-dot-... to the API as if they were an id. If you mean "take this from that bot", that is a snap.
- Never give a port both a snap and an "inputs" value. The value wins and the snap is silently dropped, so the swarm looks wired and isn't. Pick one.
- When an upstream bot produces a **list** and the downstream bot takes **one item**, run the downstream bot once per item by putting ".*" on the from side, e.g. from "chaser.drafted.*.draft_id" to "sender.draft_id". That is how "for each overdue invoice, send a reminder" is expressed. Two snaps into the same fanned-out bot iterate together on one index, so they must come from the same list — snap "x.items.*.a" and "x.items.*.b", never "x.list_one.*" and "x.list_two.*", which are not guaranteed to be the same length.
- A fanned-out bot's own outputs become a list. To feed one downstream bot afterwards, collapse it by adding "join" to the snap, e.g. from "sender.acted_on" to "notifier.message" with "join": "lines". The modes are lines (one per line), json (a JSON array), count (how many), flatten (list of lists into one list) and first.
- Set "on_error": "continue" on a bot whose failure should not lose the run — a notification at the end that nothing else reads. The run then finishes and reports that one step didn't. Do not use it on a bot the swarm exists to run.
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

// composeModel is what the composer asks for. Every direct backend
// substitutes its own model when it can't serve this provider, and Shroud
// serves it as declared — see docs/llm.md.
//
// Composing is a structured-output task over a long catalog prompt, so it
// wants a capable model rather than the cheapest one; a small model
// produces drafts the planner then rejects, which costs a second call
// anyway.
var composeModel = schema.Model{Provider: "anthropic", Name: "claude-sonnet-4-6", MaxTokens: 3000}

func composeChat(ctx context.Context, gen llm.Generator, prompt string) (string, error) {
	return gen.Generate(ctx, prompt, composeModel)
}

// composerGenerator resolves what the composer talks to.
//
// The composer used to build its own Shroud client unconditionally, which
// made "describe what you want and get a swarm" — the product's primary
// entry point — require 1Claw specifically. Someone with a Gemini key had
// a working catalog, working bots, and a compose box that returned a 500.
//
// Now it uses whatever internal/wiring chose, resolving the Shroud marker
// against the composer's own agent (not a bot's) exactly as internal/runner
// does per bot.
func (s *Server) composerGenerator() (llm.Generator, error) {
	if s.Orchestrator == nil || s.Orchestrator.LLM == nil {
		return nil, fmt.Errorf("the composer needs a model: %w", llm.ErrNoGenerator)
	}
	gen := s.Orchestrator.LLM
	if !llm.IsDeferredShroud(gen) {
		return gen, nil
	}
	if s.OneClaw != nil && s.OneClaw.Configured() {
		agentID, agentAPIKey, err := s.OneClaw.EnsureAgent(s.Orchestrator.AgentStateDir, composeAgentName,
			oneclaw.CreateAgentRequest{
				ShroudEnabled: true,
				ShroudConfig:  &oneclaw.ShroudConfig{PIIPolicy: "redact", EnableSecretRedaction: true},
			})
		if err != nil {
			return nil, fmt.Errorf("ensure composer agent: %w", err)
		}
		return llm.NewShroud(oneclaw.NewShroudClient(agentID, agentAPIKey)), nil
	}
	if fallback := gen.(*llm.DeferredShroud).Fallback; fallback != nil {
		return fallback, nil
	}
	return nil, fmt.Errorf("the composer needs a model: %w", llm.ErrNoGenerator)
}
