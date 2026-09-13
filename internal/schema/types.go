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
}

// OutputPort is a typed output on a Nanobot.
type OutputPort struct {
	Name        string `json:"name" yaml:"name"`
	Type        string `json:"type" yaml:"type"`
	Mime        string `json:"mime,omitempty" yaml:"mime,omitempty"`
	Schema      string `json:"schema,omitempty" yaml:"schema,omitempty"` // path to a JSON Schema file, for json-typed outputs
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
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
	Name       string         `json:"name" yaml:"name"`
	Type       string         `json:"type" yaml:"type"` // service.call | ai.generate | http.request | transform.render | transform.now | memory.get | memory.put | memory.search | approve | if | notify | wait
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
}

// Nanoswarm is a saved graph of nanobots snapped together.
type Nanoswarm struct {
	APIVersion string        `json:"apiVersion" yaml:"apiVersion"`
	Kind       string        `json:"kind" yaml:"kind"` // "Nanoswarm"
	Metadata   Metadata      `json:"metadata" yaml:"metadata"`
	Spec       NanoswarmSpec `json:"spec" yaml:"spec"`

	SourcePath string `json:"-" yaml:"-"`
}
