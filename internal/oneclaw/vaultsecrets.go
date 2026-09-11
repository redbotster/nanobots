package oneclaw

import "fmt"

// PutSecretRequest/SecretResponse mirror @1claw/openapi-spec@0.61.1's
// PutSecretRequest/SecretCreatedResponse/SecretResponse schemas for
// PUT|GET /v1/vaults/{vault_id}/secrets/{path} — verified against the real
// spec (unpkg.com/@1claw/openapi-spec), not guessed. Both endpoints are
// billed per-call (x402, a fraction of a cent) — callers should cache a
// secret's value rather than fetching it on every step.
type PutSecretRequest struct {
	Type  string `json:"type,omitempty"` // generic, password, api_key, ... — default "generic"
	Value string `json:"value"`
}

type SecretResponse struct {
	ID      string `json:"id"`
	Path    string `json:"path"`
	Type    string `json:"type"`
	Value   string `json:"value"`
	Version int    `json:"version"`
}

// PutSecret stores or updates the secret at path within vaultID (e.g.
// "google/refresh_token" — 1Claw's own docs example a multi-segment path
// like "db/credentials" taken as a literal, un-escaped path suffix, the same
// convention MemoryGet/MemoryPut already use).
func (c *Client) PutSecret(vaultID, path, value string) error {
	var resp struct {
		ID string `json:"id"`
	}
	return c.do("PUT", fmt.Sprintf("/v1/vaults/%s/secrets/%s", vaultID, path), PutSecretRequest{Value: value}, &resp)
}

// GetSecret retrieves and decrypts the secret at path within vaultID.
func (c *Client) GetSecret(vaultID, path string) (string, error) {
	var resp SecretResponse
	if err := c.do("GET", fmt.Sprintf("/v1/vaults/%s/secrets/%s", vaultID, path), nil, &resp); err != nil {
		return "", err
	}
	return resp.Value, nil
}
