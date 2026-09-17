package oneclaw

import (
	"fmt"
	"time"
)

// ShroudConfig is the subset of 1Claw's real ShroudConfig schema that
// schema.Guardrails/schema.Model map onto. The live schema has many more
// fields (unicode normalization, command-injection detection, etc.); we set
// the ones the blueprint's guardrails block actually declares and let 1Claw
// default the rest.
type ShroudConfig struct {
	PIIPolicy             string   `json:"pii_policy,omitempty"`
	InjectionThreshold    float64  `json:"injection_threshold,omitempty"`
	AllowedProviders      []string `json:"allowed_providers,omitempty"`
	DailyBudgetUSD        float64  `json:"daily_budget_usd,omitempty"`
	EnableSecretRedaction bool     `json:"enable_secret_redaction,omitempty"`
}

// CreateAgentRequest is the request body for POST /v1/agents. Only the
// fields Nanobots actually sets are included — the live API accepts many
// more (transaction/signing-related) that don't apply to a nanobot.
type CreateAgentRequest struct {
	Name string `json:"name"`
	// Description is what the 1Claw dashboard shows beside the name. Worth
	// sending: agents are named after their guardrail profile rather than
	// after a bot now (see runner.agentNameFor), so without this an account
	// holds several `nanobots-redact-7c1f9a` and no way to tell what any of
	// them is for.
	Description             string        `json:"description,omitempty"`
	ShroudEnabled           bool          `json:"shroud_enabled,omitempty"`
	ShroudConfig            *ShroudConfig `json:"shroud_config,omitempty"`
	SystemPrompt            string        `json:"system_prompt,omitempty"`
	ExecutionIntentsEnabled bool          `json:"execution_intents_enabled,omitempty"`
	VaultIDs                []string      `json:"vault_ids,omitempty"`
	// MemoryEnabled turns on durable key-value/semantic memory for this
	// agent. Added to @1claw/openapi-spec in 0.61.1 — see
	// docs/oneclaw-bridge.md for the gap this closed.
	MemoryEnabled bool `json:"memory_enabled,omitempty"`
}

// UpdateAgentRequest is the (partial) request body for PATCH
// /v1/agents/{agent_id}. Only the field Nanobots actually needs to flip
// post-creation is included.
type UpdateAgentRequest struct {
	MemoryEnabled *bool `json:"memory_enabled,omitempty"`
}

// Agent mirrors the subset of the live Agent object Nanobots reads back.
type Agent struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Description   string `json:"description,omitempty"`
	IsActive      bool   `json:"is_active"`
	ShroudEnabled bool   `json:"shroud_enabled"`
	MemoryEnabled bool   `json:"memory_enabled"`
	CreatedAt     string `json:"created_at,omitempty"`
}

type listAgentsResponse struct {
	Agents []Agent `json:"agents"`
}

type createAgentResponse struct {
	Agent  Agent  `json:"agent"`
	APIKey string `json:"api_key"`
}

func (c *Client) ListAgents() ([]Agent, error) {
	var resp listAgentsResponse
	if err := c.do("GET", "/v1/agents", nil, &resp); err != nil {
		return nil, err
	}
	return resp.Agents, nil
}

// agentListTTL keeps EnsureAgent's existence check to roughly one API call
// per swarm run rather than one per bot — a five-bot swarm calls EnsureAgent
// five times in a row, and the answer cannot meaningfully change in between.
const agentListTTL = 30 * time.Second

func (c *Client) listAgentsCached() ([]Agent, error) {
	c.agentsMu.Lock()
	defer c.agentsMu.Unlock()
	if c.agents != nil && time.Since(c.agentsAt) < agentListTTL {
		return c.agents, nil
	}
	agents, err := c.ListAgents()
	if err != nil {
		return nil, err
	}
	c.agents, c.agentsAt = agents, time.Now()
	return agents, nil
}

func (c *Client) invalidateAgentCache() {
	c.agentsMu.Lock()
	defer c.agentsMu.Unlock()
	c.agents, c.agentsAt = nil, time.Time{}
}

func agentExists(agents []Agent, id string) bool {
	for _, a := range agents {
		if a.ID == id {
			return true
		}
	}
	return false
}

// UpdateAgent patches an existing agent — the only current use is flipping
// memory_enabled on for an agent created before that field existed on
// CreateAgentRequest (see docs/oneclaw-bridge.md).
func (c *Client) UpdateAgent(agentID string, req UpdateAgentRequest) (*Agent, error) {
	var agent Agent
	if err := c.do("PATCH", "/v1/agents/"+agentID, req, &agent); err != nil {
		return nil, err
	}
	return &agent, nil
}

// DeleteAgent permanently removes an agent — irreversible, and the caller's
// job to confirm with a human first; this client never decides on its own
// which agents are safe to remove.
func (c *Client) DeleteAgent(agentID string) error {
	return c.do("DELETE", "/v1/agents/"+agentID, nil, nil)
}

// CreateAgent registers a new agent. The returned api_key (ocv_...) is shown
// exactly once by the live API — callers must persist it (see EnsureAgent /
// SaveAgentCredential) or lose the ability to mint that agent's JWT.
func (c *Client) CreateAgent(req CreateAgentRequest) (agent *Agent, apiKey string, err error) {
	var resp createAgentResponse
	if err := c.do("POST", "/v1/agents", req, &resp); err != nil {
		return nil, "", err
	}
	return &resp.Agent, resp.APIKey, nil
}

// EnsureAgent finds an agent by name, or creates one if none exists. On
// creation, the one-time api_key is persisted to local state (see state.go)
// so future runs can reuse it. If an agent with this name already exists on
// 1Claw but there's no local credential file for it, that key was issued to
// a run we have no record of — we can't recover it, so we return a clear
// error naming the agent rather than silently proceeding without one.
func (c *Client) EnsureAgent(stateDir, name string, req CreateAgentRequest) (agentID, apiKey string, err error) {
	agents, err := c.listAgentsCached()
	if err != nil {
		return "", "", fmt.Errorf("list agents: %w", err)
	}

	cred, haveCred, err := loadAgentCredential(stateDir, name)
	if err != nil {
		return "", "", err
	}
	// Trusting the saved credential blindly was a real trap: an agent
	// deleted on 1Claw (by anyone, including this repo's own cleanup) left
	// the local file behind, and every run of that bot then died on an
	// opaque `shroud: chat failed (401): agent key exchange failed` that no
	// amount of reading the log explains. The credential is a cache of a
	// remote fact, so check the fact.
	if haveCred && agentExists(agents, cred.AgentID) {
		return cred.AgentID, cred.APIKey, nil
	}

	// A stale credential is NOT deleted here. An api_key is shown exactly
	// once and can never be recovered, so throwing one away on the strength
	// of a listing that might have been partial or momentarily wrong is a
	// far worse failure than the 401 this is fixing. Instead fall through
	// and create a replacement — saveAgentCredential overwrites the file on
	// success, and on failure the old one is still sitting there.
	for _, a := range agents {
		if a.Name == name {
			if haveCred {
				return "", "", fmt.Errorf(
					"agent %q on 1Claw is now id=%s, but the credential saved in %s is for id=%s, which no longer exists — "+
						"an api_key is shown only once, so the current agent's key can't be recovered. "+
						"Delete it with `1claw agent delete %s` and re-run to get a fresh one",
					name, a.ID, stateDir, cred.AgentID, a.ID)
			}
			return "", "", fmt.Errorf(
				"agent %q already exists on 1Claw (id=%s) but Nanobots has no saved credential for it in %s — "+
					"its api_key was only ever shown once. Delete it with `1claw agent delete %s` and re-run, "+
					"or restore the saved credential file if you have a backup",
				name, a.ID, stateDir, a.ID)
		}
	}

	req.Name = name
	agent, key, err := c.CreateAgent(req)
	if err != nil {
		// The cap is the single most likely reason this fails, and it
		// used to reach the user as a raw JSON blob buried in a container
		// error. Say which bot wanted the agent and what actually unblocks
		// it — only bots that use Shroud, memory, or a generic 1Claw
		// binding need one at all (see internal/runner/agentneed.go), so
		// the count is smaller than the catalog and worth knowing.
		if limit, ok := AsAgentLimit(err); ok {
			return "", "", fmt.Errorf(
				"%s needs its own 1Claw agent, but %s. Free a slot by deleting an unused agent in the 1Claw app, or upgrade the plan",
				name, limit.Detail)
		}
		return "", "", fmt.Errorf("create agent %q: %w", name, err)
	}
	c.invalidateAgentCache() // the list we just read is now one agent short
	if err := saveAgentCredential(stateDir, name, agentCredential{AgentID: agent.ID, APIKey: key}); err != nil {
		return "", "", fmt.Errorf("agent %q was created (id=%s) but saving its credential locally failed: %w — "+
			"it must be deleted and recreated, since its api_key cannot be retrieved again", name, agent.ID, err)
	}
	return agent.ID, key, nil
}
