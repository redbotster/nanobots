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

// VaultLocked reports whether the vault is currently refusing secret reads
// pending passkey verification.
//
// 1Claw's vault re-locks on its own schedule, and until now the only way to
// discover that was to start a swarm and watch a bot fail several steps in,
// after earlier bots had already done real work. The lock blocks every
// Slack/GitHub/Stripe/HubSpot bot in the catalog at once, so it deserves to
// be visible before you press Run — the same argument as reporting whether
// Docker is running.
//
// It probes by reading a path that intentionally does not exist: a locked
// vault answers 403 before it ever looks the path up, an unlocked one
// answers 404. Nothing is created and no real secret is read, so this is
// safe to poll.
func (c *Client) VaultLocked(vaultID string) (locked bool, detail string) {
	_, err := c.GetSecret(vaultID, "nanobots-lock-probe-does-not-exist")
	if err == nil {
		return false, "" // someone really made that key; unlocked either way
	}
	if l, ok := AsVaultLocked(err); ok {
		return true, l.Detail
	}
	// A 404, a network blip, anything else: not evidence of a lock, and
	// guessing "locked" would nag about a problem that may not exist.
	return false, ""
}
