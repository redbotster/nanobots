package oneclaw

import "fmt"

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
	Name                    string        `json:"name"`
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
	if cred, ok, err := loadAgentCredential(stateDir, name); err != nil {
		return "", "", err
	} else if ok {
		return cred.AgentID, cred.APIKey, nil
	}

	agents, err := c.ListAgents()
	if err != nil {
		return "", "", fmt.Errorf("list agents: %w", err)
	}
	for _, a := range agents {
		if a.Name == name {
			return "", "", fmt.Errorf(
				"agent %q already exists on 1Claw (id=%s) but Nanobots has no saved credential for it in %s — "+
					"its api_key was only ever shown once. Delete it with `1claw agent delete %s` and re-run, "+
					"or restore the saved credential file if you have a backup.",
				name, a.ID, stateDir, a.ID)
		}
	}

	req.Name = name
	agent, key, err := c.CreateAgent(req)
	if err != nil {
		return "", "", fmt.Errorf("create agent %q: %w", name, err)
	}
	if err := saveAgentCredential(stateDir, name, agentCredential{AgentID: agent.ID, APIKey: key}); err != nil {
		return "", "", fmt.Errorf("agent %q was created (id=%s) but saving its credential locally failed: %w — "+
			"it must be deleted and recreated, since its api_key cannot be retrieved again", name, agent.ID, err)
	}
	return agent.ID, key, nil
}
