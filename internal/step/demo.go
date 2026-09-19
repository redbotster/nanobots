package step

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/redbotster/nanobots/internal/llm"
	"github.com/redbotster/nanobots/internal/memory"
	"github.com/redbotster/nanobots/internal/schema"
)

// Approver decides `approve` steps. Swapping this is how a caller changes
// approval behavior (auto-approve for `nanobots conform`, a real queued
// prompt for the interactive runner) without duplicating the rest of Deps.
type Approver interface {
	Approve(summary, riskTier string) (approved bool, decidedBy string, err error)
}

// AutoApprover approves everything immediately, attributed to By. Used by
// conformance tests so they never hang on a human.
type AutoApprover struct{ By string }

func (a AutoApprover) Approve(summary, riskTier string) (bool, string, error) {
	by := a.By
	if by == "" {
		by = "demo"
	}
	return true, by, nil
}

// DemoDeps implements Deps entirely from a bot's fixtures/ directory and an
// in-process memory map — no network calls, no real 1Claw. It's what
// `nanobots conform` uses, and what the WebUI's "demo mode" runs bots
// against when no 1Claw key (or no real service connection) is configured.
//
// Service calls are looked up by fixture file name "<service-id>.<op>.json";
// ai.generate by "ai.generate.json". A missing fixture is a clear error, not
// a silent empty response — a bot's conformance test should fail loudly if
// nobody wrote the fixture it needs.
type DemoDeps struct {
	FixturesDir string
	Blobstore   BlobStore
	Approver    Approver
	Clock       func() time.Time
	// SkipRealRender forces transform.render to use the plain HTML fallback
	// instead of shelling out to a host Chrome. Off by default (interactive
	// demo-mode runs want a real PDF); RunConformance turns it on so
	// `nanobots conform` / `go test` stay hermetic and don't depend on
	// whatever state the host's Chrome happens to be in.
	SkipRealRender bool
	// OnServiceCall reports each service call, so a run can record that it
	// was served from fixtures. LiveDeps has always had this; DemoDeps did
	// not, so the one configuration where *everything* is demo — a fresh
	// install with no keys at all — was the one that never showed the
	// "DEMO DATA" banner. Nil is fine.
	OnServiceCall func(svc schema.Service, op string, demo bool)
	memory        map[string]map[string]string
	// agentLoopCalls indexes into fixtures/agent_loop.json — see
	// GenerateWithTools.
	agentLoopCalls int
}

func NewDemoDeps(fixturesDir string, blobs BlobStore) *DemoDeps {
	return &DemoDeps{
		FixturesDir: fixturesDir,
		Blobstore:   blobs,
		Approver:    AutoApprover{By: "demo"},
		Clock:       time.Now,
		memory:      map[string]map[string]string{},
	}
}

func (d *DemoDeps) loadFixture(name string) (any, error) {
	path := filepath.Join(d.FixturesDir, name)
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("no fixture at %s (demo mode requires one per service.call op): %w", path, err)
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("parse fixture %s: %w", path, err)
	}
	return v, nil
}

func (d *DemoDeps) ServiceCall(svc schema.Service, op string, params map[string]any) (any, error) {
	// Unconditionally demo: that is what these Deps are. LiveDeps has to
	// decide per call; here there is nothing to decide.
	if d.OnServiceCall != nil {
		d.OnServiceCall(svc, op, true)
	}
	return d.loadFixture(fmt.Sprintf("%s.%s.json", svc.ID, op))
}

func (d *DemoDeps) AIGenerate(prompt string, model schema.Model) (string, error) {
	v, err := d.loadFixture("ai.generate.json")
	if err != nil {
		return "", err
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// GenerateWithTools plays back fixtures/agent_loop.json — a JSON array of
// turns, one per call this bot's own agent.loop step ends up making, in
// order. Each turn is either {"tool_calls":[{"id","name","arguments"}]} or
// {"content":"...","done":true}, mirroring llm.ToolCallResult directly
// rather than inventing a second shape to translate. Calling this more
// times than the fixture has turns is a fixture that doesn't cover what
// the bot actually does, so it's a clear error rather than an empty reply.
func (d *DemoDeps) GenerateWithTools(_ []llm.Message, _ []llm.ToolDef, _ schema.Model) (*llm.ToolCallResult, error) {
	v, err := d.loadFixture("agent_loop.json")
	if err != nil {
		return nil, err
	}
	turns, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("fixtures/agent_loop.json must be a JSON array of turns")
	}
	if d.agentLoopCalls >= len(turns) {
		return nil, fmt.Errorf("agent.loop called a %d%s time, but fixtures/agent_loop.json only has %d turn(s) —"+
			" add the next one the real conversation actually took",
			d.agentLoopCalls+1, ordinalSuffix(d.agentLoopCalls+1), len(turns))
	}
	raw, err := json.Marshal(turns[d.agentLoopCalls])
	if err != nil {
		return nil, err
	}
	d.agentLoopCalls++
	var parsed struct {
		Content   string `json:"content"`
		Done      bool   `json:"done"`
		ToolCalls []struct {
			ID        string `json:"id"`
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"tool_calls"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("parse fixtures/agent_loop.json turn %d: %w", d.agentLoopCalls, err)
	}
	out := &llm.ToolCallResult{Content: parsed.Content, Done: parsed.Done}
	for _, tc := range parsed.ToolCalls {
		out.ToolCalls = append(out.ToolCalls, llm.ToolCall{ID: tc.ID, Name: tc.Name, Arguments: tc.Arguments})
	}
	return out, nil
}

// ordinalSuffix is just for a readable error message ("1st", "2nd", "3rd",
// "4th") — not worth a dependency for.
func ordinalSuffix(n int) string {
	if n%100 >= 11 && n%100 <= 13 {
		return "th"
	}
	switch n % 10 {
	case 1:
		return "st"
	case 2:
		return "nd"
	case 3:
		return "rd"
	default:
		return "th"
	}
}

// WebFetch serves bots/<id>/fixtures/web.fetch.json — like every other
// DemoDeps method, the fixture is returned regardless of which URL(s) were
// actually requested, so conformance never touches the network.
func (d *DemoDeps) WebFetch(params map[string]any) (any, error) {
	return d.loadFixture("web.fetch.json")
}

func (d *DemoDeps) Render(templatePath string, data any, to string) ([]byte, string, error) {
	if !d.SkipRealRender {
		switch to {
		case "pdf":
			return RenderHTMLToPDF(templatePath, data)
		case "png":
			return RenderHTMLToPNG(templatePath, data)
		}
	}
	html, err := RenderHTML(templatePath, data)
	return html, "text/html", err
}

func (d *DemoDeps) Now() string { return d.Clock().UTC().Format(time.RFC3339) }

func (d *DemoDeps) MemoryGet(namespace, key string) (string, bool, error) {
	ns, ok := d.memory[namespace]
	if !ok {
		return "", false, nil
	}
	v, ok := ns[key]
	return v, ok, nil
}

// MemoryRecall answers from a fixture, so a bot that uses recall can be
// conformance-tested offline like every other step. `fixtures/memory.recall.json`
// holds either a plain string or {"<question>": "<answer>"}; anything not
// covered answers empty, which is a legitimate "nothing known".
func (d *DemoDeps) MemoryRecall(namespace, question string) (string, error) {
	raw, err := os.ReadFile(filepath.Join(d.FixturesDir, "memory.recall.json"))
	if err != nil {
		// No fixture means this bot has nothing to answer with offline.
		// Report it as "no recall here" rather than as an empty answer, so
		// a bot with a *required* recall step fails conformance instead of
		// passing on silence — the same contract it would meet at runtime
		// on a key/value backend. A bot with `optional: true` degrades and
		// still conforms.
		return "", memory.ErrNoRecall
	}
	var byQuestion map[string]string
	if err := json.Unmarshal(raw, &byQuestion); err == nil {
		if a, ok := byQuestion[question]; ok {
			return a, nil
		}
		return "", nil
	}
	var single string
	if err := json.Unmarshal(raw, &single); err == nil {
		return single, nil
	}
	return "", nil
}

// MemoryRemember is a no-op in demo mode: there is nothing deriving from
// observations, and failing here would make an offline conformance run
// depend on a server.
func (d *DemoDeps) MemoryRemember(namespace, text string) error { return nil }

func (d *DemoDeps) MemoryPut(namespace, key, value string) error {
	if d.memory[namespace] == nil {
		d.memory[namespace] = map[string]string{}
	}
	d.memory[namespace][key] = value
	return nil
}

func (d *DemoDeps) Approve(summary, riskTier string) (bool, string, error) {
	return d.Approver.Approve(summary, riskTier)
}

func (d *DemoDeps) Notify(message, channel string) error { return nil }

func (d *DemoDeps) Blobs() BlobStore { return d.Blobstore }
