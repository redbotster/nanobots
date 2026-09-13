package api

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/redbotster/nanobots/internal/schema"
)

// Every LLM bot declares an optional `instructions` port whose `default:` is
// the suggestion it ships with — "Anything mentioning data loss or billing is
// top priority". Editing that suggestion meant opening the bot's
// nanobot.yaml in a text editor, or overriding it per-swarm in the builder's
// inspector, which only changes that one swarm.
//
// This is the catalog-wide edit: change what the bot does by default,
// everywhere it's used, from the bot card.

type setBotInstructionsRequest struct {
	Instructions string `json:"instructions"`
}

// maxInstructionsLen bounds what lands in a prompt. Long enough for real
// guidance, short enough that it can't crowd out the bot's own rules — the
// precedence block is only as strong as its position, and a wall of user
// text works against that.
const maxInstructionsLen = 2000

func (s *Server) handleSetBotInstructions(w http.ResponseWriter, r *http.Request) {
	botID := r.PathValue("id")

	var req setBotInstructionsRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	text := strings.TrimSpace(req.Instructions)
	if len(text) > maxInstructionsLen {
		writeError(w, http.StatusBadRequest,
			fmt.Errorf("instructions are %d characters; keep them under %d so they can't crowd out the bot's own rules", len(text), maxInstructionsLen))
		return
	}
	if strings.Contains(text, "\n") {
		// The port is a single-line YAML scalar. Rather than silently
		// mangling a multi-line paste, say so.
		writeError(w, http.StatusBadRequest,
			fmt.Errorf("instructions must be a single line — join them into one sentence or separate with '. '"))
		return
	}

	// filepath.Base, not the raw id: this builds a filesystem path from a
	// URL segment.
	botPath := filepath.Join(s.BotsDir, filepath.Base(botID), "nanobot.yaml")
	nb, err := schema.LoadNanobot(botPath)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	if !hasInstructionsPort(nb) {
		writeError(w, http.StatusBadRequest,
			fmt.Errorf("bot %q has no instructions port — only bots with an ai.generate step take one", botID))
		return
	}

	// Read the current default before overwriting it. What it shipped with
	// is what lets the Team say "you changed this" and offer to put it
	// back. The record itself is written after a successful save, below, so
	// a failed write can't leave a record of a change that never happened.
	previous := ""
	for _, port := range nb.Spec.Ports.Inputs {
		if port.Name == "instructions" {
			previous = port.Default
		}
	}

	raw, err := os.ReadFile(botPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	updated, err := setInstructionsDefaultInYAML(raw, text)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	// Never write something that won't load back.
	var check schema.Nanobot
	if err := yaml.Unmarshal(updated, &check); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("editing produced invalid YAML: %w", err))
		return
	}
	if err := os.WriteFile(botPath, updated, 0o644); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	if s.Team != nil {
		if shipped, known := s.Team.Shipped(filepath.Base(botID)); known && shipped == text {
			// Put back to what it shipped with — it's no longer tuned.
			_ = s.Team.Forget(filepath.Base(botID))
		} else if text != previous {
			_ = s.Team.RecordTuned(filepath.Base(botID), previous)
		}
	}

	fresh, err := schema.LoadNanobot(botPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, botSummaryOf(fresh, filepath.Base(botID)))
}

func hasInstructionsPort(nb *schema.Nanobot) bool {
	for _, p := range nb.Spec.Ports.Inputs {
		if p.Name == "instructions" {
			return true
		}
	}
	return false
}

// setInstructionsDefaultInYAML rewrites the `default:` line inside the
// `- name: instructions` input port, leaving every other line — and every
// comment — exactly as it was. Same surgical approach as
// setServiceConnectionInYAML, for the same reason: these files are
// hand-written and full of explanation worth keeping.
func setInstructionsDefaultInYAML(raw []byte, text string) ([]byte, error) {
	lines := strings.Split(string(raw), "\n")
	itemIndent := -1
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if itemIndent == -1 {
			if trimmed == "- name: instructions" {
				itemIndent = len(line) - len(strings.TrimLeft(line, " "))
			}
			continue
		}
		lineIndent := len(line) - len(strings.TrimLeft(line, " "))
		if trimmed != "" && lineIndent <= itemIndent {
			break // past the end of this port's own block
		}
		if strings.HasPrefix(trimmed, "default:") {
			indent := line[:len(line)-len(strings.TrimLeft(line, " "))]
			lines[i] = indent + "default: " + yamlQuote(text)
			return []byte(strings.Join(lines, "\n")), nil
		}
	}
	return nil, fmt.Errorf("the instructions port has no default: line to update")
}

// yamlQuote emits a double-quoted YAML scalar. Written out rather than
// reached for from the yaml package because that would re-encode the whole
// document and lose the comments this edit exists to preserve.
func yamlQuote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
