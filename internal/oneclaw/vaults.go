package oneclaw

import "fmt"

// Vault mirrors the object returned by GET/POST /v1/vaults.
type Vault struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Description   string `json:"description,omitempty"`
	CreatedBy     string `json:"created_by,omitempty"`
	CreatedByType string `json:"created_by_type,omitempty"`
	CreatedAt     string `json:"created_at,omitempty"`
}

type listVaultsResponse struct {
	Vaults []Vault `json:"vaults"`
}

func (c *Client) ListVaults() ([]Vault, error) {
	var resp listVaultsResponse
	if err := c.do("GET", "/v1/vaults", nil, &resp); err != nil {
		return nil, err
	}
	return resp.Vaults, nil
}

func (c *Client) CreateVault(name string) (*Vault, error) {
	var v Vault
	if err := c.do("POST", "/v1/vaults", map[string]string{"name": name}, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

// EnsureVault returns the vault with the given name, creating it if it
// doesn't exist yet. Idempotent across repeated `nanobots up` runs.
func (c *Client) EnsureVault(name string) (*Vault, error) {
	vaults, err := c.ListVaults()
	if err != nil {
		return nil, fmt.Errorf("list vaults: %w", err)
	}
	for i := range vaults {
		if vaults[i].Name == name {
			return &vaults[i], nil
		}
	}
	v, err := c.CreateVault(name)
	if err != nil {
		return nil, fmt.Errorf("create vault %q: %w", name, err)
	}
	return v, nil
}
