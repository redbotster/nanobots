package oneclaw

type oauthConnectRequest struct {
	ProviderSlug  string   `json:"provider_slug"`
	Scopes        []string `json:"scopes,omitempty"`
	RedirectAfter string   `json:"redirect_after,omitempty"`
}

type oauthConnectResponse struct {
	AuthorizationURL string `json:"authorization_url"`
}

// OAuthConnect starts 1Claw's native OAuth flow for a provider on this
// agent, returning the URL a human opens to grant access. Human-only by
// design — see blueprint §3.3.
func (c *Client) OAuthConnect(agentID, providerSlug string, scopes []string, redirectAfter string) (string, error) {
	var resp oauthConnectResponse
	req := oauthConnectRequest{ProviderSlug: providerSlug, Scopes: scopes, RedirectAfter: redirectAfter}
	if err := c.do("POST", "/v1/agents/"+agentID+"/oauth/connect", req, &resp); err != nil {
		return "", err
	}
	return resp.AuthorizationURL, nil
}
