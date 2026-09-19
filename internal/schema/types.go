// Package schema defines the Go types for the Nanobot and Nanoswarm resources
// described in NANOBOTS-BLUEPRINT.md. These are the source of truth for the
// generated JSON Schema in schemas/ and for everything the planner type-checks
// against.
package schema

// PortType is the set of types a port can carry. Assignability between these
// is what the planner's snap type-checker enforces.
type PortType string

const (
	PortString   PortType = "string"
	PortDatetime PortType = "datetime"
	PortBoolean  PortType = "boolean"
	PortJSON     PortType = "json"
	PortFile     PortType = "file"
	PortEvent    PortType = "event"
	// list<T> ports are written as "list<string>", "list<json>", etc. and
	// parsed with ParsePortType.
)

// InputPort is a typed input on a Nanobot.
type InputPort struct {
	Name     string `json:"name" yaml:"name"`
	Type     string `json:"type" yaml:"type"`
	Default  string `json:"default,omitempty" yaml:"default,omitempty"`
	Required bool   `json:"required,omitempty" yaml:"required,omitempty"`
	// MapsTo is only meaningful on a Nanoswarm's own Spec.Ports (a swarm
	// nested elsewhere as a `swarm:` node) — "<bot-id>.<port>", the inner
	// bot input this boundary port's value is handed to. Empty on every
	// Nanobot's own ports, which have nothing to map to.
	MapsTo string `json:"maps_to,omitempty" yaml:"maps_to,omitempty"`
}

// OutputPort is a typed output on a Nanobot.
type OutputPort struct {
	Name        string `json:"name" yaml:"name"`
	Type        string `json:"type" yaml:"type"`
	Mime        string `json:"mime,omitempty" yaml:"mime,omitempty"`
	Schema      string `json:"schema,omitempty" yaml:"schema,omitempty"` // path to a JSON Schema file, for json-typed outputs
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
	// MapsTo is only meaningful on a Nanoswarm's own Spec.Ports: the inner
	// "<bot-id>.<port>" this boundary output's value comes from. See
	// InputPort.MapsTo.
	MapsTo string `json:"maps_to,omitempty" yaml:"maps_to,omitempty"`
}

// Ports groups a Nanobot's inputs and outputs.
type Ports struct {
	Inputs  []InputPort  `json:"inputs,omitempty" yaml:"inputs,omitempty"`
	Outputs []OutputPort `json:"outputs,omitempty" yaml:"outputs,omitempty"`
}

// Harness selects the agent loop that drives a bot.
type Harness struct {
	// Type is what drives the bot. Three values are implemented here:
	//   bare     — fixed steps, no LLM, no browser
	//   llm      — fixed steps that call an LLM; same image as bare, because
	//              ai.generate is a callback to nanobotd, not an in-container
	//              model
	//   openclaw — needs a real browser (transform.render to pdf/png)
	// The blueprint's wider vocabulary (claude-code | opencode | openclaude |
	// hermes) names dynamic agent loops that don't exist in this build;
	// EnsureHarnessImage rejects them by name rather than substituting.
	Type       string `json:"type" yaml:"type"`
	Version    string `json:"version,omitempty" yaml:"version,omitempty"`
	Entrypoint string `json:"entrypoint,omitempty" yaml:"entrypoint,omitempty"`
	// Execution forces this bot into a container on every swarm that runs
	// it, overriding the runner's own in-process-by-default heuristic
	// (internal/runner.runsInProcess). Two values: "" (the default — the
	// runner decides) or "container".
	//
	// Deliberately one-directional. The heuristic already picks in-process
	// whenever nothing needs isolating and a container whenever something
	// does (a real headless browser); an "inprocess" override could only
	// ever mean forcing a browser-driving bot out of the one sandbox that
	// isolation argument actually applies to, which is not a knob this
	// build offers. CheckExecution rejects anything else written here.
	Execution string `json:"execution,omitempty" yaml:"execution,omitempty"`
}

// Model is the default LLM configuration for ai.generate steps.
type Model struct {
	Provider    string  `json:"provider,omitempty" yaml:"provider,omitempty"`
	Name        string  `json:"name,omitempty" yaml:"name,omitempty"`
	MaxTokens   int     `json:"max_tokens,omitempty" yaml:"max_tokens,omitempty"`
	Temperature float64 `json:"temperature,omitempty" yaml:"temperature,omitempty"`
}

// ConnectionMethod is Nanobots' own abstraction over how a service gets
// connected — a layer on top of whatever 1Claw exposes natively. See
// docs/connections.md.
type ConnectionMethod string

const (
	ConnectionOAuth1Claw  ConnectionMethod = "oauth_1claw"  // 1Claw's own OAuth provider registry
	ConnectionOAuthNative ConnectionMethod = "oauth_native" // a provider-specific OAuth client Nanobots implements directly (e.g. Google)
	ConnectionBrowser     ConnectionMethod = "browser"      // 1Claw Browser Bridge
	ConnectionAPIKeyVault ConnectionMethod = "api_key_vault"
	ConnectionDemo        ConnectionMethod = "demo" // fixture data, no real network calls
)

// Service is an external system a bot reads or writes.
type Service struct {
	ID         string           `json:"id" yaml:"id"`
	Provider   string           `json:"provider" yaml:"provider"`
	Scopes     []string         `json:"scopes,omitempty" yaml:"scopes,omitempty"`
	Required   bool             `json:"required,omitempty" yaml:"required,omitempty"`
	Connection ConnectionMethod `json:"connection,omitempty" yaml:"connection,omitempty"` // resolved by the connection registry if empty
}

// Step is one entry in a Nanobot's spec.steps pipeline. Only the fields
// relevant to its Type are meaningful; see internal/step for the interpreter.
type Step struct {
	Name string `json:"name" yaml:"name"`
	// Type is one of step.Types(). That list is the authority; this comment
	// used to name http.request, memory.search, `if` and wait, none of
	// which the interpreter has ever implemented — a vocabulary that
	// existed only in a comment, and in the JSON Schema generated from it.
	Type       string         `json:"type" yaml:"type"`
	Service    string         `json:"service,omitempty" yaml:"service,omitempty"`
	Op         string         `json:"op,omitempty" yaml:"op,omitempty"`
	Params     map[string]any `json:"params,omitempty" yaml:"params,omitempty"`
	PromptFile string         `json:"prompt_file,omitempty" yaml:"prompt_file,omitempty"`
	Inputs     map[string]any `json:"inputs,omitempty" yaml:"inputs,omitempty"`
	Template   string         `json:"template,omitempty" yaml:"template,omitempty"`
	To         string         `json:"to,omitempty" yaml:"to,omitempty"`
	Key        string         `json:"key,omitempty" yaml:"key,omitempty"`
	// Query is memory.recall's question, in plain language — "what does
	// this person usually escalate?" — as opposed to Key, which names one
	// stored value.
	Query string `json:"query,omitempty" yaml:"query,omitempty"`
	// Optional marks a step whose result improves the bot but isn't
	// required. It exists for memory.recall: the default memory backend is
	// key/value and cannot answer questions, so a bot that merely benefits
	// from recall must be able to run without it, while one that genuinely
	// depends on it should still fail loudly. The bot declares which it is
	// rather than the engine guessing.
	Optional bool   `json:"optional,omitempty" yaml:"optional,omitempty"`
	Value    string `json:"value,omitempty" yaml:"value,omitempty"`
	// Data is transform.pick's payload — unlike Value (a plain string, used
	// by memory.put), this can be any YAML shape (a string, a number, an
	// object) so a step can build something like an event payload directly.
	Data   any    `json:"data,omitempty" yaml:"data,omitempty"`
	Output string `json:"output,omitempty" yaml:"output,omitempty"`
	// Outputs extracts several named output ports directly from this one
	// step's own result in a single pass — e.g. a lookup step producing
	// both a file and its id. Each value is a template resolved against the
	// same ctx as everything else, with steps.<this-step>.output already
	// available (see internal/step/interpret.go). Output (singular) binds
	// the step's whole result to one port; Outputs (plural) is for pulling
	// several fields out of it at once. A step may use either or both.
	Outputs  map[string]string `json:"outputs,omitempty" yaml:"outputs,omitempty"`
	Summary  string            `json:"summary,omitempty" yaml:"summary,omitempty"`
	RiskTier string            `json:"risk_tier,omitempty" yaml:"risk_tier,omitempty"`
	// Equals is stop.if's comparison: when Value resolves to the same text
	// as Equals, the bot ends there having done nothing, and Summary says
	// why. Text on both sides on purpose — the thing being compared is an
	// id, an etag or a timestamp, and "is this the same one as last time"
	// does not need a type system.
	Equals string `json:"equals,omitempty" yaml:"equals,omitempty"`
}

// Guardrails are constraints a bot declares about itself.
type Guardrails struct {
	PII                 string   `json:"pii,omitempty" yaml:"pii,omitempty"` // redact | block | allow
	InjectionThreshold  float64  `json:"injection_threshold,omitempty" yaml:"injection_threshold,omitempty"`
	MaxRuntimeSecs      int      `json:"max_runtime_secs,omitempty" yaml:"max_runtime_secs,omitempty"`
	NetworkEgress       []string `json:"network_egress,omitempty" yaml:"network_egress,omitempty"`
	WritesAllowed       []string `json:"writes_allowed,omitempty" yaml:"writes_allowed,omitempty"`
	DailyBudgetUSD      float64  `json:"daily_budget_usd,omitempty" yaml:"daily_budget_usd,omitempty"`
	ApprovalRequiredFor []string `json:"approval_required_for,omitempty" yaml:"approval_required_for,omitempty"`
}

// Resources describes the container footprint.
type Resources struct {
	Preset string `json:"preset,omitempty" yaml:"preset,omitempty"` // small | medium | large
	Memory string `json:"memory,omitempty" yaml:"memory,omitempty"`
	CPU    string `json:"cpu,omitempty" yaml:"cpu,omitempty"`
	Image  string `json:"image,omitempty" yaml:"image,omitempty"`
}

// Metadata is the Kubernetes-flavoured metadata block shared by both kinds.
type Metadata struct {
	Name        string   `json:"name" yaml:"name"`
	Version     string   `json:"version,omitempty" yaml:"version,omitempty"`
	Description string   `json:"description,omitempty" yaml:"description,omitempty"`
	Tags        []string `json:"tags,omitempty" yaml:"tags,omitempty"`
	Author      string   `json:"author,omitempty" yaml:"author,omitempty"`
	License     string   `json:"license,omitempty" yaml:"license,omitempty"`
	Owner       string   `json:"owner,omitempty" yaml:"owner,omitempty"`
}

// NanobotSpec is the spec block of a Nanobot resource.
type NanobotSpec struct {
	Harness    Harness    `json:"harness" yaml:"harness"`
	Model      Model      `json:"model,omitempty" yaml:"model,omitempty"`
	Services   []Service  `json:"services,omitempty" yaml:"services,omitempty"`
	Ports      Ports      `json:"ports" yaml:"ports"`
	Steps      []Step     `json:"steps" yaml:"steps"`
	Guardrails Guardrails `json:"guardrails,omitempty" yaml:"guardrails,omitempty"`
	Resources  Resources  `json:"resources,omitempty" yaml:"resources,omitempty"`
}

// Nanobot is a single brick: apiVersion/kind/metadata/spec, Helm-chart shaped.
type Nanobot struct {
	APIVersion string      `json:"apiVersion" yaml:"apiVersion"`
	Kind       string      `json:"kind" yaml:"kind"` // "Nanobot"
	Metadata   Metadata    `json:"metadata" yaml:"metadata"`
	Spec       NanobotSpec `json:"spec" yaml:"spec"`

	// SourcePath is the directory the nanobot.yaml was loaded from — not part
	// of the schema, used by the planner/runner to resolve relative paths
	// (bot.md, prompts/, templates/, fixtures/).
	SourcePath string `json:"-" yaml:"-"`
}

// BotRef references a bot instance inside a swarm's bots[] list.
type BotRef struct {
	ID     string         `json:"id" yaml:"id"`
	Use    string         `json:"use,omitempty" yaml:"use,omitempty"`   // registry ref: name@version
	Path   string         `json:"path,omitempty" yaml:"path,omitempty"` // local path, alternative to use:
	Inputs map[string]any `json:"inputs,omitempty" yaml:"inputs,omitempty"`
	// Swarm nests a whole other Nanoswarm as this one node, alternative to
	// Use/Path: a path to another swarm's YAML, resolved relative to this
	// swarm's own directory. The referenced swarm must declare Spec.Ports —
	// its typed boundary — since that's what this node's inputs and outputs
	// type-check against. See internal/planner.InlineNestedSwarms.
	Swarm string `json:"swarm,omitempty" yaml:"swarm,omitempty"`
	// OnError decides whether this bot failing ends the run.
	//
	//	stop      (default) the run fails, as it always has
	//	continue  the run carries on; anything downstream is skipped
	//
	// Per bot *instance* rather than per bot, because only the swarm knows
	// whether a failure matters: notify failing after get-paid has already
	// sent every reminder is a missed Slack message, not a failed run,
	// while the same bot elsewhere might be the whole point.
	//
	// See docs/error-policy.md.
	OnError string `json:"on_error,omitempty" yaml:"on_error,omitempty"`
	// Retry re-runs this bot up to N more times if it fails. 0 (the
	// default) never retries.
	//
	// Opt-in per instance, and the hazard is the whole reason: a retry
	// re-runs the *entire bot*, including anything it already did. A bot
	// that sent an email and then failed on its last step will send that
	// email again. Only mark a bot that is safe to run twice.
	//
	// An approval decline is never retried — that is a decision, not a
	// fault, and asking again until someone says yes is not a retry.
	Retry int `json:"retry,omitempty" yaml:"retry,omitempty"`
	// RetryBackoff is how long to wait before each retry, as a Go duration
	// string ("5s", "1m"). Empty means retry immediately, which is the
	// existing behaviour and stays the default: most of this catalog's
	// transient failures are a container race, not rate limiting, and an
	// immediate retry is what a human hitting Run again would do anyway.
	// Only meaningful alongside Retry > 0; see CheckRetry.
	RetryBackoff string `json:"retry_backoff,omitempty" yaml:"retry_backoff,omitempty"`
	// When gates this bot instance on one of its own resolved inputs — the
	// swarm-level equivalent of 1Claw Automations' condition step, so a
	// swarm and an automation read alike. Empty (the default) always runs.
	//
	// Only "{{inputs.<port>}}" is legal on the left, comparing to a literal
	// or another template: "{{inputs.amount}} > 500". No operator at all is
	// a bare truthy check. False skips this bot exactly the way
	// on_error: continue does — everything downstream that depends on it is
	// skipped too, and it is not a failure.
	//
	// A condition on an *upstream* bot's output reaches here only once it is
	// wired to a port with a snap — this can't reach into a bot it has no
	// input from, on purpose: a condition on data that never flowed into
	// this bot is a condition on nothing, however the human reading the
	// swarm meant it.
	//
	// See docs/when.md and internal/planner.CheckWhen.
	When string `json:"when,omitempty" yaml:"when,omitempty"`
	// Fallback names another catalog bot ("name@version") to run in this
	// one's place if it still fails after Retry is exhausted — a live
	// service call degrading to a fixture, with the run saying so rather
	// than staying silent about it.
	//
	// The fallback bot must declare the exact same output ports, name for
	// name and type for type, as this one: a downstream snap type-checked
	// against this bot's ports, and a fallback that changed the shape would
	// make that type-check a lie. Every required input port it declares
	// must already be one this bot instance has, because the fallback runs
	// with this instance's own resolved inputs — nothing is re-wired for it.
	//
	// Not itself retried, and not chained: a fallback that also fails ends
	// the run (or continues past it, per on_error) exactly as if there were
	// no fallback. A declined approval or a stopped run is never handed to
	// a fallback either — those aren't failures a substitute bot can fix.
	//
	// See docs/error-policy.md and internal/planner.CheckFallback.
	Fallback string `json:"fallback,omitempty" yaml:"fallback,omitempty"`
	// Loop re-runs this bot instance in place, feeding its own previous
	// output back as its own next input, for pagination and polling: "keep
	// fetching next_page until there isn't one, at most 20 times". nil (the
	// default) runs once, same as everything else.
	//
	// See docs/loop.md and internal/planner.CheckLoop.
	Loop *Loop `json:"loop,omitempty" yaml:"loop,omitempty"`
	// Execution forces this bot instance into a container in this swarm
	// specifically, overriding both the runner's heuristic and the bot's
	// own Harness.Execution — the swarm author's call on a bot they know
	// is fine in-process everywhere else but not for what this swarm feeds
	// it. Same two values and the same one-way restriction as
	// Harness.Execution; see its doc comment and internal/planner.CheckExecution.
	Execution string `json:"execution,omitempty" yaml:"execution,omitempty"`
}

// Loop bounds a bot instance's self-repetition. See BotRef.Loop.
type Loop struct {
	// Max is the ceiling on iterations, required and capped (see
	// internal/planner.maxLoop) — an unbounded "while" against someone
	// else's API is not a thing this runs unattended.
	Max int `json:"max" yaml:"max"`
	// Feed maps this bot's own input port names to its own output port
	// names: after an iteration, the named output's value becomes the
	// named input's value for the next one. Both sides must be ports this
	// bot itself declares — nothing else is available to feed back.
	Feed map[string]string `json:"feed,omitempty" yaml:"feed,omitempty"`
	// Until stops the loop once true, checked after each iteration against
	// that iteration's own {{outputs.<port>}} — the loop's mirror of
	// when:'s {{inputs.<port>}}, same operator set. Empty means loop
	// exactly Max times.
	Until string `json:"until,omitempty" yaml:"until,omitempty"`
}

// OnError values.
const (
	OnErrorStop     = "stop"
	OnErrorContinue = "continue"
)

// Snap is a typed connection from one bot's output port to another's input.
type Snap struct {
	From string `json:"from" yaml:"from"` // "<bot-id>.<port>[.<field>...]"
	To   string `json:"to" yaml:"to"`
	// Join collapses a fanned-out bot's list output into one value — the
	// inverse of the `.*` marker. lines | json | count | flatten | first.
	// See internal/planner/join.go and docs/fan-out.md.
	Join string `json:"join,omitempty" yaml:"join,omitempty"`
}

// Trigger is how a swarm starts a run.
type Trigger struct {
	Type     string `json:"type" yaml:"type"` // cron | webhook | manual | event
	Expr     string `json:"expr,omitempty" yaml:"expr,omitempty"`
	Timezone string `json:"timezone,omitempty" yaml:"timezone,omitempty"`
}

// Deploy is where a swarm runs.
type Deploy struct {
	Target string         `json:"target" yaml:"target"` // local | 1claw | kubernetes | apple
	OnClaw map[string]any `json:"onclaw,omitempty" yaml:"onclaw,omitempty"`
}

// SwarmDefaults are swarm-wide defaults every bot inherits unless it overrides.
type SwarmDefaults struct {
	Model      Model      `json:"model,omitempty" yaml:"model,omitempty"`
	Guardrails Guardrails `json:"guardrails,omitempty" yaml:"guardrails,omitempty"`
	Resources  Resources  `json:"resources,omitempty" yaml:"resources,omitempty"`
}

// NanoswarmSpec is the spec block of a Nanoswarm resource.
type NanoswarmSpec struct {
	Defaults SwarmDefaults  `json:"defaults,omitempty" yaml:"defaults,omitempty"`
	Vars     map[string]any `json:"vars,omitempty" yaml:"vars,omitempty"`
	Trigger  Trigger        `json:"trigger" yaml:"trigger"`
	Bots     []BotRef       `json:"bots" yaml:"bots"`
	Snaps    []Snap         `json:"snaps,omitempty" yaml:"snaps,omitempty"`
	Deploy   Deploy         `json:"deploy" yaml:"deploy"`
	// Ports declares this swarm's own external interface, for when it is
	// nested elsewhere as a single `swarm:` node. Empty means this swarm
	// cannot be nested — a swarm with no declared boundary has no typed
	// contract to snap into, the same reason an undeclared bot port can't
	// be snapped either.
	Ports Ports `json:"ports,omitempty" yaml:"ports,omitempty"`
}

// Nanoswarm is a saved graph of nanobots snapped together.
type Nanoswarm struct {
	APIVersion string        `json:"apiVersion" yaml:"apiVersion"`
	Kind       string        `json:"kind" yaml:"kind"` // "Nanoswarm"
	Metadata   Metadata      `json:"metadata" yaml:"metadata"`
	Spec       NanoswarmSpec `json:"spec" yaml:"spec"`

	SourcePath string `json:"-" yaml:"-"`
}
