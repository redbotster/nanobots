package lab

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/redbotster/nanobots/internal/llm"
)

type action string

const (
	actionDelegate action = "delegate"
	actionStatus   action = "status"
	actionAnswer   action = "answer"
)

// decision is the one JSON object route asks the model for.
type decision struct {
	Action action `json:"action"`
	Role   string `json:"role,omitempty"`
	Task   string `json:"task,omitempty"`
	Text   string `json:"text,omitempty"`
}

// route asks the configured model what to do with the conversation so
// far, restated in full — the same reason compose.go's prompts restate
// the whole bot catalog every call: Generate has no memory of its own
// between calls, so anything the decision needs to see has to be in this
// one prompt.
func route(ctx context.Context, gen llm.Generator, history []turn, roles []string) (decision, error) {
	raw, err := gen.Generate(ctx, routePrompt(history, roles), labModel)
	if err != nil {
		return decision{}, err
	}
	return parseDecision(raw)
}

func routePrompt(history []turn, roles []string) string {
	var b strings.Builder
	b.WriteString("You are Lab, the orchestrator for a nanobots Team (see context/TEAM-LAB-DESIGN.md). ")
	b.WriteString("A human is chatting with you. For their latest message, decide exactly one of three things and respond with exactly one JSON object — nothing before it, nothing after it, no markdown fence.\n\n")

	b.WriteString("1. Delegate a task to a Team member, who works in their own persistent workspace (a real git worktree of the nanobots repo) and can read the bot catalog, author or edit bots and swarms, and run the nanobots CLI — never anything against a real account until a human separately runs or approves it:\n")
	b.WriteString(`   {"action": "delegate", "role": "<short-kebab-case-role>", "task": "<a clear, self-contained task description>"}` + "\n")
	if len(roles) > 0 {
		fmt.Fprintf(&b, "   Existing roles, each with their own workspace and history: %s. Reuse one when the task fits it.\n", strings.Join(roles, ", "))
	} else {
		b.WriteString("   No roles exist yet. Invent a short, sensible one (e.g. \"backend-engineer\", \"designer\") that fits the task.\n")
	}
	b.WriteString("\n2. Report a role's recent work, when the human is asking what a role has done rather than asking for new work:\n")
	b.WriteString(`   {"action": "status", "role": "<an existing role>"}` + "\n\n")

	b.WriteString("3. Just answer in chat — a greeting, a clarifying question, general conversation, or anything that plainly isn't a task for the team:\n")
	b.WriteString(`   {"action": "answer", "text": "<your reply>"}` + "\n\n")

	if len(history) > 1 {
		b.WriteString("Conversation so far:\n")
		for _, t := range history[:len(history)-1] {
			fmt.Fprintf(&b, "%s: %s\n", t.who, t.text)
		}
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "The human just said: %q\n\n", history[len(history)-1].text)
	b.WriteString("Respond with exactly one JSON object as described above.")
	return b.String()
}

func parseDecision(raw string) (decision, error) {
	cleaned := strings.TrimSpace(raw)
	cleaned = strings.TrimPrefix(cleaned, "```json")
	cleaned = strings.TrimPrefix(cleaned, "```")
	cleaned = strings.TrimSuffix(cleaned, "```")
	cleaned = strings.TrimSpace(cleaned)

	var d decision
	if err := json.Unmarshal([]byte(cleaned), &d); err != nil {
		return decision{}, fmt.Errorf("model response was not valid JSON: %w", err)
	}
	switch d.Action {
	case actionDelegate:
		if d.Role == "" || d.Task == "" {
			return decision{}, fmt.Errorf("a delegate decision needs both role and task: %q", raw)
		}
	case actionStatus:
		if d.Role == "" {
			return decision{}, fmt.Errorf("a status decision needs a role: %q", raw)
		}
	case actionAnswer:
		if d.Text == "" {
			return decision{}, fmt.Errorf("an answer decision needs text: %q", raw)
		}
	default:
		return decision{}, fmt.Errorf("unrecognized action %q: %q", d.Action, raw)
	}
	return d, nil
}
