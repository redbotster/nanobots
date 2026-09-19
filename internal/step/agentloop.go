package step

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/redbotster/nanobots/internal/llm"
	"github.com/redbotster/nanobots/internal/schema"
)

// runAgentLoop drives one agent.loop step: the model decides, iteration by
// iteration, which of the step's declared Tools to call, sees each result,
// and keeps going until it produces a final answer or hits MaxIterations.
// See docs/agent-loop.md.
//
// Every tool call goes through the exact same Deps dispatch a fixed step
// would use for the same operation — deps.ServiceCall for a Service/Op
// tool, deps.WebFetch/MemoryGet/MemoryPut for a Builtin one — so
// `connection: demo` answers from fixtures here exactly as it does
// everywhere else, and a write-capable tool gets the same deps.Approve
// gate bots/email-send-approved already uses inline before its own send,
// not a second, competing safety mechanism.
//
// log is the same closure Interpret's main loop already builds — one line
// per tool call, so a run's log shows what an agent.loop step actually did
// with the same granularity every other step type gets.
func runAgentLoop(nb *schema.Nanobot, s schema.Step, ctx map[string]any, deps Deps, log func(stepName, format string, a ...any)) (any, error) {
	goal := fmt.Sprint(resolveValue(s.Goal, ctx))
	if strings.TrimSpace(goal) == "" {
		return nil, fmt.Errorf("agent.loop needs a goal")
	}
	if s.MaxIterations < 1 {
		// Belongs to internal/planner.CheckAgentLoop at plan time; caught
		// again here so a bot loaded outside a swarm (a direct
		// nanobots run, a unit test) can't skip the check.
		return nil, fmt.Errorf("agent.loop needs max_iterations of at least 1")
	}

	toolDefs := make([]llm.ToolDef, len(s.Tools))
	for i, t := range s.Tools {
		toolDefs[i] = llm.ToolDef{Name: t.Name, Description: t.Description, Parameters: t.Parameters}
	}
	messages := []llm.Message{{Role: "user", Content: goal}}
	var transcript []string

	for iter := 1; iter <= s.MaxIterations; iter++ {
		result, err := deps.GenerateWithTools(messages, toolDefs, nb.Spec.Model)
		if err != nil {
			return nil, fmt.Errorf("agent.loop: iteration %d: %w\ntranscript so far:\n%s",
				iter, err, strings.Join(transcript, "\n"))
		}
		if result.Done {
			log(s.Name, "agent.loop finished after %d of %d max iteration(s)", iter, s.MaxIterations)
			return result.Content, nil
		}
		if len(result.ToolCalls) == 0 {
			return nil, fmt.Errorf("agent.loop: iteration %d: the model returned neither a final answer nor a"+
				" tool call\ntranscript so far:\n%s", iter, strings.Join(transcript, "\n"))
		}
		messages = append(messages, llm.Message{Role: "assistant", ToolCalls: result.ToolCalls})
		for _, call := range result.ToolCalls {
			line, toolResult, err := runOneAgentToolCall(nb, s, call, deps)
			transcript = append(transcript, fmt.Sprintf("iteration %d: %s", iter, line))
			log(s.Name, "%s", line)
			if err != nil {
				return nil, fmt.Errorf("agent.loop: iteration %d: %w\ntranscript so far:\n%s",
					iter, err, strings.Join(transcript, "\n"))
			}
			messages = append(messages, llm.Message{Role: "tool", ToolCallID: call.ID, Content: toolResult})
		}
	}
	return nil, fmt.Errorf("agent.loop: hit its cap of %d iteration(s) without a final answer\ntranscript:\n%s",
		s.MaxIterations, strings.Join(transcript, "\n"))
}

// runOneAgentToolCall dispatches one model-chosen tool call and returns a
// human-readable log line, the tool's result as a JSON string ready to go
// back to the model as a "tool" message, and an error only for something
// the loop itself cannot recover from (a declined approval, a malformed
// declared tool) — a failing tool call is reported back to the model as
// its own result rather than aborting the loop, since "that didn't work,
// try something else" is a normal thing for a model mid-task to see.
func runOneAgentToolCall(nb *schema.Nanobot, s schema.Step, call llm.ToolCall, deps Deps) (line string, toolResultJSON string, err error) {
	tool, ok := findAgentTool(s.Tools, call.Name)
	if !ok {
		return fmt.Sprintf("%s(%s) -> refused: not a declared tool", call.Name, call.Arguments),
			`{"error":"not a declared tool"}`, nil
	}
	var args map[string]any
	if strings.TrimSpace(call.Arguments) != "" {
		if jsonErr := json.Unmarshal([]byte(call.Arguments), &args); jsonErr != nil {
			return fmt.Sprintf("%s(%s) -> refused: arguments were not valid json: %v", call.Name, call.Arguments, jsonErr),
				`{"error":"arguments were not valid json"}`, nil
		}
	}
	if tool.Writes != "" {
		summary := fmt.Sprintf("%s wants to %s: %s(%s)", nb.Metadata.Name, tool.Writes, tool.Name, call.Arguments)
		approved, decidedBy, aerr := deps.Approve(summary, "high")
		if aerr != nil {
			return "", "", fmt.Errorf("approval for %s: %w", tool.Name, aerr)
		}
		if !approved {
			return fmt.Sprintf("%s(%s) -> declined (decided_by=%s)", tool.Name, call.Arguments, decidedBy), "",
				fmt.Errorf("tool %q not approved (decided_by=%s)", tool.Name, decidedBy)
		}
	}
	out, terr := dispatchAgentTool(nb, tool, args, deps)
	if terr != nil {
		return fmt.Sprintf("%s(%s) -> error: %v", tool.Name, call.Arguments, terr),
			fmt.Sprintf(`{"error":%s}`, jsonString(terr.Error())), nil
	}
	resultJSON, jerr := json.Marshal(out)
	if jerr != nil {
		return fmt.Sprintf("%s(%s) -> error encoding its own result: %v", tool.Name, call.Arguments, jerr),
			fmt.Sprintf(`{"error":%s}`, jsonString(jerr.Error())), nil
	}
	return fmt.Sprintf("%s(%s) -> %d bytes", tool.Name, call.Arguments, len(resultJSON)), string(resultJSON), nil
}

func findAgentTool(tools []schema.AgentTool, name string) (schema.AgentTool, bool) {
	for _, t := range tools {
		if t.Name == name {
			return t, true
		}
	}
	return schema.AgentTool{}, false
}

// dispatchAgentTool runs one declared tool against the model's own chosen
// arguments — the same three primitives every fixed step already has:
// service.call, web.fetch, memory.get/put. No transform.* here yet; those
// are pure computation over values already in a step's ctx; a tool call
// needs its result handed straight back to the model as its own message,
// which is a different enough shape that it is left for when a real bot
// actually needs it rather than built speculatively (docs/agent-loop.md).
func dispatchAgentTool(nb *schema.Nanobot, tool schema.AgentTool, args map[string]any, deps Deps) (any, error) {
	if tool.Service != "" {
		svc, ok := findService(nb, tool.Service)
		if !ok {
			return nil, fmt.Errorf("tool %q names service %q, which this bot does not declare", tool.Name, tool.Service)
		}
		return deps.ServiceCall(svc, tool.Op, args)
	}
	switch tool.Builtin {
	case "web.fetch":
		return deps.WebFetch(args)
	case "memory.get":
		key, _ := args["key"].(string)
		val, found, err := deps.MemoryGet(nb.Metadata.Name, key)
		if err != nil {
			return nil, err
		}
		return map[string]any{"value": val, "found": found}, nil
	case "memory.put":
		key, _ := args["key"].(string)
		val, _ := args["value"].(string)
		if err := deps.MemoryPut(nb.Metadata.Name, key, val); err != nil {
			return nil, err
		}
		return map[string]any{"ok": true}, nil
	default:
		return nil, fmt.Errorf("tool %q declares neither service/op nor a recognised builtin"+
			` ("web.fetch", "memory.get", "memory.put")`, tool.Name)
	}
}

// jsonString quotes s as a JSON string literal — used for the one place an
// error message itself needs to become valid JSON without pulling in a
// throwaway struct just to marshal one field.
func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
