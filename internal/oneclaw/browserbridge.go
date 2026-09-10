package oneclaw

// This file is the 1Claw Browser Bridge client: pairing a device, defining
// credential bindings, and opening an agent's gated browser session. It's
// real and tested against the live API (see docs/browser-bridge.md), but —
// per the finding logged there — Google refuses sign-in on any
// CDP/automation-controlled browser outright, so it isn't wired into the two
// example bots' Gmail/Drive access in this build. It remains the intended
// connection method for services that don't do that (Stripe, HubSpot,
// X/LinkedIn dashboards, etc.) in later tranches.

type PairDeviceRequest struct {
	Label         string `json:"label"`
	PublicKeyPin  string `json:"public_key_pin"`
	BridgeVersion string `json:"bridge_version,omitempty"`
	Platform      string `json:"platform,omitempty"`
}

type PairDeviceResult struct {
	DeviceID   string `json:"device_id"`
	Label      string `json:"label"`
	Credential string `json:"credential"` // bb_... — shown once
}

// PairDevice registers this machine with the browser bridge vault, minting
// its bb_ credential. Shown once by the live API; callers must persist it
// themselves (Nanobots stores it the same way as agent credentials, see
// state.go) since it can't be retrieved again.
func (c *Client) PairDevice(req PairDeviceRequest) (*PairDeviceResult, error) {
	var resp PairDeviceResult
	if err := c.do("POST", "/v1/browser/devices", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

type CreateBrowserCredentialRequest struct {
	Label        string   `json:"label"`
	VaultID      string   `json:"vault_id"`
	SecretPath   string   `json:"secret_path"`
	LoginURL     string   `json:"login_url"`
	AllowedHosts []string `json:"allowed_hosts"`
	SSOHosts     []string `json:"sso_hosts,omitempty"`
}

type BrowserCredential struct {
	ID string `json:"id"`
}

// CreateBrowserCredential defines which vault secret may be typed into which
// hosts — a human-only policy decision (see docs/browser-bridge.md).
func (c *Client) CreateBrowserCredential(req CreateBrowserCredentialRequest) (*BrowserCredential, error) {
	var resp BrowserCredential
	if err := c.do("POST", "/v1/browser/credentials", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

type browserSessionRequest struct {
	AgentID  string `json:"agent_id"`
	ClientID string `json:"client_id"`
}

type BrowserSession struct {
	SessionID    string `json:"session_id"`
	SessionToken string `json:"session_token"`
	ExpiresAt    string `json:"expires_at"`
}

// OpenBrowserSession opens a gated browser session for an agent, scoping
// what that agent's CDP connection is allowed to touch.
func (c *Client) OpenBrowserSession(agentID, clientID string) (*BrowserSession, error) {
	var resp BrowserSession
	req := browserSessionRequest{AgentID: agentID, ClientID: clientID}
	if err := c.do("POST", "/v1/agents/"+agentID+"/browser/sessions", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
