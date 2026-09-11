package foundry

import (
	"context"

	"github.com/redbotster/nanobots/internal/schema"
)

// BriefInput is everything an Agent needs to know to author one new bot.
// It's handed to Agent implementations as structured data, not a
// pre-built prompt string, so each implementation (a real CLI coding
// agent, or a test fake) decides for itself how — or whether — to turn it
// into a prompt. See agent_claude.go's BuildBrief for the real one.
type BriefInput struct {
	Request           string
	MissingCapability string
	SuggestedInputs   []schema.InputPort
	SuggestedOutputs  []schema.OutputPort
	ExistingBotIDs    []string // "id@version", collision-avoidance only
}

// Event is one unit of live progress an Agent reports while it works —
// fed straight into the Job's Run.Log so it streams over the same SSE
// mechanism a swarm run's log already uses.
type Event struct {
	Phase string // "text" | "tool" | "result"
	Msg   string
}

// Agent authors one new bot inside workDir (a git worktree; which files it
// actually touched is checked afterward by verifySandbox) and reports
// progress on events as it goes. The only shipped implementation is
// ClaudeCLIAgent (agent_claude.go), which runs entirely inside a Docker
// container (harness/foundry-agent) — that boundary, not this interface or
// any CLI permission flag, is what actually confines an untrusted,
// tool-using coding session. This was a real course-correction, not the
// original assumption: CLI flags like --allowedTools scoped to one command
// were tried first and confirmed, empirically, not to restrict what the
// tool could run at all; --disallowedTools does work as a real block, and
// is used in agent_claude.go as defense in depth on top of the container,
// never as the primary boundary.
//
// A second coding-agent CLI (OpenCode or otherwise) is meant to be a
// second implementation of this same interface, built the same way
// ClaudeCLIAgent was — verified against a real invocation of that tool
// first, not guessed from its docs, since that's exactly the class of
// assumption that turned out wrong here.
type Agent interface {
	Run(ctx context.Context, workDir string, in BriefInput, events chan<- Event) error
}
