package oneclaw

// CredentialSource selects where a binding's credential comes from — either
// set inline (rare, avoid for anything real) or by reference into a vault
// path the agent's policies already grant it.
type CredentialSource struct {
	Type    string `json:"type"` // "inline" | "vault_ref"
	VaultID string `json:"vault_id,omitempty"`
	Path    string `json:"path,omitempty"`
}

type CreateBindingRequest struct {
	Name             string            `json:"name"`
	BindingType      string            `json:"binding_type"` // http | graphql | postgres | mysql | redis | grpc | smtp | cloud_sdk | s3 | custom
	Config           map[string]any    `json:"config,omitempty"`
	CredentialSource *CredentialSource `json:"credential_source,omitempty"`
}

// Binding mirrors the subset of the live Binding object Nanobots reads back.
type Binding struct {
	ID          string `json:"id"`
	AgentID     string `json:"agent_id"`
	BindingType string `json:"binding_type"`
	Name        string `json:"name"`
	IsActive    bool   `json:"is_active"`
}

func (c *Client) CreateBinding(agentID string, req CreateBindingRequest) (*Binding, error) {
	var b Binding
	if err := c.do("POST", "/v1/agents/"+agentID+"/bindings", req, &b); err != nil {
		return nil, err
	}
	return &b, nil
}
