package oneclaw

import (
	"fmt"
	"net/url"
)

// Connectors are 1Claw's pre-built integrations — Gmail, Slack, GitHub and
// the rest — and they remove this project's single largest obstacle.
//
// Until now, pointing a bot at a real account meant registering your own
// OAuth application with the provider: a Google Cloud project, a consent
// screen, a "Desktop app" client id. Thirteen of the catalog's bots are
// Google bots, and every one of them stayed on demo fixtures until someone
// did that. It is an afternoon of unrelated work for a developer and a
// wall for everyone else.
//
// 1Claw already holds reviewed OAuth apps for these providers. Installing a
// preset creates the agent's binding and hands back an authorization URL;
// the human completes one round trip in a browser and the agent can make
// real calls. No provider console, no client secret on disk.
//
// It also closes internal/step's oldest TODO. LiveDeps.ServiceCall has
// always been able to execute against "a binding named svc.ID on the
// agent", with a comment saying nothing provisions one. This is what
// provisions one.

// ConnectorPreset is one entry in 1Claw's connector catalogue.
type ConnectorPreset struct {
	Slug        string `json:"slug"`
	DisplayName string `json:"display_name"`
	Description string `json:"description"`
	Category    string `json:"category"`
	Provider    string `json:"provider_slug"`
	// OAuthScopes is what the preset asks for; RequiredScopes is the
	// subset an install may not drop.
	OAuthScopes    []string `json:"oauth_scopes"`
	RequiredScopes []string `json:"required_scopes"`
	BindingType    string   `json:"binding_type"`
	BaseURL        string   `json:"base_url"`
	AllowedHosts   []string `json:"allowed_hosts"`
	RequiresOAuth  bool     `json:"requires_oauth"`
}

type presetListResponse struct {
	Presets []ConnectorPreset `json:"presets"`
}

// ListConnectorPresets returns the connector catalogue.
func (c *Client) ListConnectorPresets() ([]ConnectorPreset, error) {
	var resp presetListResponse
	if err := c.do("GET", "/v1/connectors/presets", nil, &resp); err != nil {
		return nil, err
	}
	return resp.Presets, nil
}

type installConnectorRequest struct {
	BindingName string   `json:"binding_name,omitempty"`
	Scopes      []string `json:"scopes,omitempty"`
}

// InstalledConnector is what an install returns.
type InstalledConnector struct {
	BindingID   string `json:"binding_id"`
	BindingName string `json:"binding_name"`
	PresetSlug  string `json:"preset_slug"`
	// AuthorizationURL is where the human has to go. Absent for a
	// connector that takes a pasted API key instead of an OAuth round trip
	// — which is why callers must check it rather than assume.
	AuthorizationURL string `json:"authorization_url"`
	// NextStep is 1Claw's own words for what remains. Passed through
	// rather than reworded: it knows what it is about to ask for.
	NextStep string `json:"next_step"`
}

// InstallConnector creates the agent's binding for a preset.
//
// bindingName is what the binding is called on the agent, and it matters:
// LiveDeps.ServiceCall executes against a binding named after the bot's own
// service id, so installing gmail as "gmail" is what makes a bot's
// `services: [{id: gmail}]` resolve.
//
// scopes may narrow the preset's list but never widen it — the preset's
// scopes are the reviewed part of a one-click install, and 1Claw refuses
// anything broader.
func (c *Client) InstallConnector(agentID, slug, bindingName string, scopes []string) (*InstalledConnector, error) {
	if agentID == "" || slug == "" {
		return nil, fmt.Errorf("install connector: need an agent and a preset slug")
	}
	var out InstalledConnector
	path := fmt.Sprintf("/v1/agents/%s/connectors/%s/install",
		url.PathEscape(agentID), url.PathEscape(slug))
	if err := c.do("POST", path, installConnectorRequest{BindingName: bindingName, Scopes: scopes}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// InstalledConnectorStatus is one connector on an agent, and whether the
// OAuth round trip has actually happened. An install creates the binding;
// the binding is not usable until someone finishes in a browser, and
// conflating the two is how a bot ends up failing on a credential that
// looks present.
type InstalledConnectorStatus struct {
	BindingID   string `json:"binding_id"`
	BindingName string `json:"binding_name"`
	PresetSlug  string `json:"preset_slug"`
	Connected   bool   `json:"connected"`
}

type connectorListResponse struct {
	Connectors []InstalledConnectorStatus `json:"connectors"`
}

// ListAgentConnectors returns the connectors installed on an agent.
func (c *Client) ListAgentConnectors(agentID string) ([]InstalledConnectorStatus, error) {
	var resp connectorListResponse
	if err := c.do("GET", "/v1/agents/"+url.PathEscape(agentID)+"/connectors", nil, &resp); err != nil {
		return nil, err
	}
	return resp.Connectors, nil
}

// OAuthAppCredential is one provider's registered application, as 1Claw
// reports it. The client secret is write-only and never comes back.
type OAuthAppCredential struct {
	Provider string `json:"provider_slug"`
	ClientID string `json:"client_id"`
}

type appCredentialListResponse struct {
	Credentials []OAuthAppCredential `json:"credentials"`
}

type saveAppCredentialRequest struct {
	Provider     string `json:"provider_slug"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	RedirectURI  string `json:"redirect_uri,omitempty"`
}

// SaveOAuthAppCredentials registers a provider's OAuth application against
// an agent.
//
// This is the step that makes a connector installable, and it is worth
// being blunt about what it does not remove: 1Claw does not ship shared
// OAuth apps. Every provider here — Google, Slack, GitHub, X, Notion,
// Discord — answers an install with "No app credentials configured;
// register your OAuth app credentials first" until this is called. So the
// Google Cloud project is still required.
//
// What it does buy is that the app is registered once, in one place, and
// 1Claw runs every provider's round trip and holds the secret. Without it,
// this repo implements and maintains a separate OAuth client per provider
// — which it already does for Google, X and LinkedIn — and each one is a
// refresh-token dance nobody wants to write again.
//
// The secret goes straight to 1Claw and is never written to disk here.
func (c *Client) SaveOAuthAppCredentials(agentID, provider, clientID, clientSecret, redirectURI string) error {
	if agentID == "" || provider == "" || clientID == "" || clientSecret == "" {
		return fmt.Errorf("save oauth app credentials: need an agent, provider, client id and secret")
	}
	return c.do("POST", "/v1/agents/"+url.PathEscape(agentID)+"/oauth/app-credentials",
		saveAppCredentialRequest{
			Provider: provider, ClientID: clientID, ClientSecret: clientSecret, RedirectURI: redirectURI,
		}, nil)
}

// ListOAuthAppCredentials returns which providers this agent has an app
// registered for. Secrets are never included.
func (c *Client) ListOAuthAppCredentials(agentID string) ([]OAuthAppCredential, error) {
	var resp appCredentialListResponse
	if err := c.do("GET", "/v1/agents/"+url.PathEscape(agentID)+"/oauth/app-credentials", nil, &resp); err != nil {
		return nil, err
	}
	return resp.Credentials, nil
}
