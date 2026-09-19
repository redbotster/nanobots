package planner

import (
	"fmt"

	"github.com/redbotster/nanobots/internal/schema"
)

// maxAgentLoopIterations mirrors maxLoop (inputs.go) for the same reason:
// an unbounded "keep trying tools" against a model's own judgement is not
// a thing this runs unattended.
const maxAgentLoopIterations = 20

// CheckAgentLoop validates every agent.loop step against a bot's own
// declared services — the one thing a bot instance's swarm context adds
// that internal/step.runAgentLoop's own runtime checks (goal, iteration
// count) can't see ahead of time.
func CheckAgentLoop(rs *ResolvedSwarm) []error {
	var out []error
	for id, rb := range rs.Bots {
		if rb.Nanobot == nil {
			continue
		}
		for _, s := range rb.Nanobot.Spec.Steps {
			if s.Type != "agent.loop" {
				continue
			}
			where := fmt.Sprintf("bot %q's %q step", id, s.Name)
			if s.MaxIterations < 1 || s.MaxIterations > maxAgentLoopIterations {
				out = append(out, fmt.Errorf("%s has max_iterations: %d — it must be between 1 and %d",
					where, s.MaxIterations, maxAgentLoopIterations))
			}
			names := map[string]bool{}
			for _, tool := range s.Tools {
				if tool.Name == "" {
					out = append(out, fmt.Errorf("%s declares a tool with no name", where))
					continue
				}
				if names[tool.Name] {
					out = append(out, fmt.Errorf("%s declares tool %q more than once", where, tool.Name))
				}
				names[tool.Name] = true

				hasService := tool.Service != ""
				hasBuiltin := tool.Builtin != ""
				switch {
				case hasService && hasBuiltin:
					out = append(out, fmt.Errorf(
						"%s's tool %q names both a service and a builtin — a tool is one or the other",
						where, tool.Name))
				case hasService:
					if _, ok := findServiceByID(rb.Nanobot.Spec.Services, tool.Service); !ok {
						out = append(out, fmt.Errorf(
							"%s's tool %q names service %q, which this bot does not declare",
							where, tool.Name, tool.Service))
					}
					if tool.Op == "" {
						out = append(out, fmt.Errorf("%s's tool %q names a service but no op", where, tool.Name))
					}
				case hasBuiltin:
					switch tool.Builtin {
					case "web.fetch", "memory.get", "memory.put":
					default:
						out = append(out, fmt.Errorf(
							`%s's tool %q has builtin: %q — the only ones internal/step.runAgentLoop`+
								` knows how to dispatch are "web.fetch", "memory.get" and "memory.put"`,
							where, tool.Name, tool.Builtin))
					}
				default:
					out = append(out, fmt.Errorf(
						"%s's tool %q names neither a service/op nor a builtin — nothing would run when it's called",
						where, tool.Name))
				}
			}
		}
	}
	return out
}

func findServiceByID(services []schema.Service, id string) (schema.Service, bool) {
	for _, s := range services {
		if s.ID == id {
			return s, true
		}
	}
	return schema.Service{}, false
}
