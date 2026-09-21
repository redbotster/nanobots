// The catalog-wide edit for a bot's guardrails — the same reasoning
// bot_instructions.go already applies to a bot's suggested instructions,
// extended to the seven fields in spec.guardrails: change what a bot is
// allowed to do, everywhere it's used, from its Team card.
//
// Loosening a guardrail is not the same size of decision as tuning a
// suggestion, so this validates harder than bot_instructions.go does:
// pii must be one of Shroud's own three values, an approval_required_for
// entry must name a step the bot actually has (or "*"), and a
// max_runtime_secs on a bot with an approve step must still leave the
// person answering it as long as runner.ApprovalTimeout already promises
// — see TestABotThatAsksAPersonWaitsLongEnoughForOne
// (internal/contract/approvalbudget_test.go), which this exists to keep
// nobody from silently violating through the UI.
package api

import (
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/redbotster/nanobots/internal/runner"
	"github.com/redbotster/nanobots/internal/schema"
)

type setBotGuardrailsRequest struct {
	PII                 string   `json:"pii"`
	InjectionThreshold  float64  `json:"injection_threshold"`
	MaxRuntimeSecs      int      `json:"max_runtime_secs"`
	DailyBudgetUSD      float64  `json:"daily_budget_usd"`
	NetworkEgress       []string `json:"network_egress"`
	WritesAllowed       []string `json:"writes_allowed"`
	ApprovalRequiredFor []string `json:"approval_required_for"`
}

func (r setBotGuardrailsRequest) toGuardrails() schema.Guardrails {
	return schema.Guardrails{
		PII:                 r.PII,
		InjectionThreshold:  r.InjectionThreshold,
		MaxRuntimeSecs:      r.MaxRuntimeSecs,
		DailyBudgetUSD:      r.DailyBudgetUSD,
		NetworkEgress:       cleanStrings(r.NetworkEgress),
		WritesAllowed:       cleanStrings(r.WritesAllowed),
		ApprovalRequiredFor: cleanStrings(r.ApprovalRequiredFor),
	}
}

// cleanStrings drops blank entries and trims whitespace — a pasted list
// with a stray empty line must not become a guardrail entry that is
// silently never going to match anything real.
func cleanStrings(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// validateGuardrails rejects a request before it ever touches disk. Every
// check here is something that would otherwise fail later, in a place
// harder to read than a 400 from Settings: a live 1Claw call, a silently
// no-op approval_required_for entry, or the exact invariant
// TestABotThatAsksAPersonWaitsLongEnoughForOne exists to enforce.
func validateGuardrails(nb *schema.Nanobot, g schema.Guardrails) error {
	switch g.PII {
	case "redact", "block", "allow":
	default:
		return fmt.Errorf("pii must be %q, %q, or %q, not %q", "redact", "block", "allow", g.PII)
	}
	if g.InjectionThreshold < 0 || g.InjectionThreshold > 1 {
		return fmt.Errorf("injection_threshold must be between 0 and 1, not %v", g.InjectionThreshold)
	}
	if g.MaxRuntimeSecs < 0 {
		return fmt.Errorf("max_runtime_secs cannot be negative")
	}
	if g.DailyBudgetUSD < 0 {
		return fmt.Errorf("daily_budget_usd cannot be negative")
	}
	if g.MaxRuntimeSecs > 0 && hasApproveStep(nb) {
		budget := g.MaxRuntimeSecs
		want := int(runner.ApprovalTimeout.Seconds())
		if budget < want {
			return fmt.Errorf(
				"this bot stops to ask a person, so max_runtime_secs must be at least %d (the approval window) — %d would kill the run before anyone could answer",
				want, budget)
		}
	}
	steps := map[string]bool{}
	for _, s := range nb.Spec.Steps {
		steps[s.Name] = true
	}
	for _, name := range g.ApprovalRequiredFor {
		if name == "*" || steps[name] {
			continue
		}
		return fmt.Errorf("approval_required_for names step %q, which this bot has no step called — it would silently require approval for nothing", name)
	}
	return nil
}

func hasApproveStep(nb *schema.Nanobot) bool {
	for _, s := range nb.Spec.Steps {
		if s.Type == "approve" {
			return true
		}
	}
	return false
}

func (s *Server) handleSetBotGuardrails(w http.ResponseWriter, r *http.Request) {
	botID := r.PathValue("id")

	var req setBotGuardrailsRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	botPath, err := s.catalogBotManifest(botID)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	nb, err := schema.LoadNanobot(botPath)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}

	next := req.toGuardrails()
	if err := validateGuardrails(nb, next); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	previous := nb.Spec.Guardrails

	raw, err := os.ReadFile(botPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	updated, err := setGuardrailsInYAML(raw, next)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	// Never write something that won't load back — and confirm it loads
	// back to exactly what was asked for, not merely to *something*
	// parseable, since a line-surgery bug that silently wrote the wrong
	// value would otherwise pass this check.
	var check schema.Nanobot
	if err := yaml.Unmarshal(updated, &check); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("editing produced invalid YAML: %w", err))
		return
	}
	if !guardrailsEqual(check.Spec.Guardrails, next) {
		writeError(w, http.StatusInternalServerError, fmt.Errorf(
			"editing did not produce the requested guardrails (got %+v, wanted %+v) — refusing to write it", check.Spec.Guardrails, next))
		return
	}

	if err := os.WriteFile(botPath, updated, 0o644); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	if s.Team != nil {
		if shipped, known := s.Team.ShippedGuardrails(botID); known && guardrailsEqual(shipped, next) {
			_ = s.Team.ForgetGuardrails(botID)
		} else if !guardrailsEqual(previous, next) {
			_ = s.Team.RecordGuardrailsTuned(botID, previous)
		}
	}

	fresh, err := schema.LoadNanobot(botPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, botSummaryOf(fresh, botID))
}

func guardrailsEqual(a, b schema.Guardrails) bool {
	return a.PII == b.PII &&
		a.InjectionThreshold == b.InjectionThreshold &&
		a.MaxRuntimeSecs == b.MaxRuntimeSecs &&
		a.DailyBudgetUSD == b.DailyBudgetUSD &&
		stringsEqual(a.NetworkEgress, b.NetworkEgress) &&
		stringsEqual(a.WritesAllowed, b.WritesAllowed) &&
		stringsEqual(a.ApprovalRequiredFor, b.ApprovalRequiredFor)
}

func stringsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// setGuardrailsInYAML rewrites the guardrails: block's seven fields in
// place, leaving every other line — every comment, every other block —
// exactly as it was. Same surgical approach as setInstructionsDefaultInYAML,
// scaled from one line to seven fields that can each be absent, a single
// line, or (a flow list) spread across several.
func setGuardrailsInYAML(raw []byte, g schema.Guardrails) ([]byte, error) {
	lines := strings.Split(string(raw), "\n")
	blockStart, blockEnd, childIndent, found := guardrailsBlockRange(lines)
	if !found {
		return nil, fmt.Errorf("this bot has no guardrails: block to edit")
	}

	type field struct {
		key   string
		value string // "" (with keep=false) removes the line entirely
		keep  bool
	}
	fields := []field{
		{"pii", g.PII, g.PII != ""},
		{"injection_threshold", formatFloat(g.InjectionThreshold), g.InjectionThreshold != 0},
		{"max_runtime_secs", strconv.Itoa(g.MaxRuntimeSecs), g.MaxRuntimeSecs != 0},
		{"daily_budget_usd", formatFloat(g.DailyBudgetUSD), g.DailyBudgetUSD != 0},
		{"network_egress", flowList(g.NetworkEgress), len(g.NetworkEgress) > 0},
		{"writes_allowed", flowList(g.WritesAllowed), len(g.WritesAllowed) > 0},
		{"approval_required_for", flowList(g.ApprovalRequiredFor), len(g.ApprovalRequiredFor) > 0},
	}

	// Applied one at a time, each re-locating the block's current end: an
	// earlier field's insert or removal shifts every line number after it.
	for _, f := range fields {
		start, end, exists := fieldSpan(lines, blockStart, blockEnd, childIndent, f.key)
		newLine := strings.Repeat(" ", childIndent) + f.key + ": " + f.value
		switch {
		case f.keep && exists:
			lines = replaceLines(lines, start, end, []string{newLine})
			blockEnd += 1 - (end - start)
		case f.keep && !exists:
			lines = replaceLines(lines, blockEnd, blockEnd, []string{newLine})
			blockEnd++
		case !f.keep && exists:
			lines = replaceLines(lines, start, end, nil)
			blockEnd -= end - start
		}
	}
	return []byte(strings.Join(lines, "\n")), nil
}

// guardrailsBlockRange finds the top-level `guardrails:` line and the
// range of its children — [blockStart, blockEnd) — by indentation, the
// same boundary rule setInstructionsDefaultInYAML already uses for one
// port's block. childIndent is the indentation of whichever line
// established the block's contents; if the block is currently empty,
// it falls back to blockIndent+2, this repo's own consistent style.
func guardrailsBlockRange(lines []string) (blockStart, blockEnd, childIndent int, found bool) {
	blockIndent := -1
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if blockIndent == -1 {
			if trimmed == "guardrails:" {
				blockIndent = len(line) - len(strings.TrimLeft(line, " "))
				blockStart = i
				found = true
			}
			continue
		}
		if trimmed == "" {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		if indent <= blockIndent {
			blockEnd = i
			if childIndent == 0 {
				childIndent = blockIndent + 2
			}
			return blockStart, blockEnd, childIndent, found
		}
		if childIndent == 0 {
			childIndent = indent
		}
	}
	if found {
		blockEnd = len(lines)
		if childIndent == 0 {
			childIndent = blockIndent + 2
		}
	}
	return blockStart, blockEnd, childIndent, found
}

// fieldSpan finds one child key's line range within [blockStart+1,
// blockEnd), including any continuation — a block list's `- item` lines,
// or a flow list's `[`...`]` spread across several lines. The rule is
// indentation: the key's own line, plus every following line that is
// indented deeper than childIndent, tolerating a blank line only when a
// deeper-indented line follows it (a blank line *inside* a continuation),
// stopping at the first line back at childIndent or shallower (the next
// sibling key) or at blockEnd.
//
// Trailing blank lines are deliberately excluded from the span even
// though the scan passes over them to look ahead — found live: the blank
// line this repo's own style leaves between guardrails: and the next
// top-level key was getting silently absorbed into whichever field
// happened to be last, so replacing that field's one line also deleted
// the blank line after it. lastContent tracks the real end explicitly
// rather than reusing wherever the lookahead loop stopped.
func fieldSpan(lines []string, blockStart, blockEnd, childIndent int, key string) (start, end int, found bool) {
	prefix := key + ":"
	for i := blockStart + 1; i < blockEnd; i++ {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed != prefix && !strings.HasPrefix(trimmed, prefix+" ") {
			continue
		}
		indent := len(lines[i]) - len(strings.TrimLeft(lines[i], " "))
		if indent != childIndent {
			continue
		}
		cursor := i + 1
		lastContent := cursor
		for cursor < blockEnd {
			t := strings.TrimSpace(lines[cursor])
			if t == "" {
				cursor++
				continue
			}
			lineIndent := len(lines[cursor]) - len(strings.TrimLeft(lines[cursor], " "))
			if lineIndent <= childIndent {
				break
			}
			cursor++
			lastContent = cursor
		}
		return i, lastContent, true
	}
	return 0, 0, false
}

// replaceLines splices replacement in place of lines[start:end] — the one
// primitive both an update (non-empty replacement) and a removal (nil
// replacement) reduce to.
func replaceLines(lines []string, start, end int, replacement []string) []string {
	out := make([]string, 0, len(lines)-(end-start)+len(replacement))
	out = append(out, lines[:start]...)
	out = append(out, replacement...)
	out = append(out, lines[end:]...)
	return out
}

// flowList renders a single-line YAML flow sequence — the style every
// guardrail list in this catalog already uses. Editing a field that used
// to span several lines (a multi-line flow list, or a block list) always
// collapses it to this one canonical line; everything outside that one
// field's span is untouched.
func flowList(items []string) string {
	if len(items) == 0 {
		return "[]"
	}
	quoted := make([]string, len(items))
	for i, s := range items {
		quoted[i] = yamlFlowItem(s)
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

// yamlFlowItem quotes a flow-sequence item only when it needs it — this
// catalog's own style leaves plain hostnames and step names bare, and
// requoting every entry would turn a one-line diff into a whole-line
// rewrite for no reason.
func yamlFlowItem(s string) string {
	if s == "" {
		return `""`
	}
	// "*" (used bare in every catalog approval_required_for today) is the
	// most important case this catches: unquoted, YAML reads it as an
	// alias reference, not the literal character.
	needsQuote := strings.ContainsAny(s, `:#{}[],&*!|>'"%@`+"`")
	if !needsQuote {
		for _, r := range s {
			if r <= ' ' {
				needsQuote = true
				break
			}
		}
	}
	if !needsQuote {
		return s
	}
	return yamlQuote(s)
}

// formatFloat renders a guardrail number the way a human would type it —
// "0.7", not "0.7000000000000001" or "7e-01".
func formatFloat(f float64) string {
	return strconv.FormatFloat(f, 'g', -1, 64)
}
