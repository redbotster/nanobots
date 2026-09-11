// Package hubspot is a minimal, real HubSpot CRM API client for exactly the
// ops this build's bots need: finding a contact by email, and creating or
// updating one. Like Slack/GitHub/Stripe, a HubSpot private-app token never
// expires and needs no OAuth dance — paste it in once (WebUI Settings, or
// `nanobots connect hubspot`) and it's stored as a 1Claw vault secret, never
// on local disk or inside a bot container.
//
// The catalog (NANOBOTS-CATALOG.md bricks 16/17) says "HubSpot or
// Salesforce" — this build ships HubSpot only. One real CRM integration
// beats two half-built ones; Salesforce's OAuth2 + per-org instance URLs is
// a genuinely separate, larger integration, not a small addition to this.
package hubspot

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

var apiBase = "https://api.hubapi.com"

type Client struct {
	HTTPClient *http.Client
	Token      string
}

func NewClient(token string) *Client {
	return &Client{HTTPClient: &http.Client{Timeout: 15 * time.Second}, Token: token}
}

// Contact is the shape bots/lead-enricher and lead-router's fixtures use.
type Contact struct {
	ID         string            `json:"id"`
	Email      string            `json:"email"`
	Properties map[string]string `json:"properties,omitempty"`
}

type rawContact struct {
	ID         string            `json:"id"`
	Properties map[string]string `json:"properties"`
}

func (c *Client) do(method, path string, body []byte) ([]byte, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, apiBase+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("hubspot: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("hubspot: %s %s failed (%d): %s", method, path, resp.StatusCode, truncate(raw))
	}
	return raw, nil
}

// ContactSearch implements `contacts.search`: finds a contact by exact
// email match, returning nil (not an error) when no contact exists — a
// lead-enricher bot uses that to decide whether to create one.
func (c *Client) ContactSearch(email string) (*Contact, error) {
	body, _ := json.Marshal(map[string]any{
		"filterGroups": []map[string]any{{
			"filters": []map[string]string{{"propertyName": "email", "operator": "EQ", "value": email}},
		}},
		"limit": 1,
	})
	raw, err := c.do(http.MethodPost, "/crm/v3/objects/contacts/search", body)
	if err != nil {
		return nil, fmt.Errorf("contacts.search: %w", err)
	}
	var resp struct {
		Results []rawContact `json:"results"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("contacts.search: parse response: %w", err)
	}
	if len(resp.Results) == 0 {
		return nil, nil
	}
	r := resp.Results[0]
	return &Contact{ID: r.ID, Email: r.Properties["email"], Properties: r.Properties}, nil
}

// ContactUpsert implements `contacts.upsert`: creates a contact with the
// given properties (must include "email"), or updates it if one with that
// email already exists.
func (c *Client) ContactUpsert(properties map[string]string) (*Contact, error) {
	email := properties["email"]
	if email == "" {
		return nil, fmt.Errorf("contacts.upsert: properties.email is required")
	}
	existing, err := c.ContactSearch(email)
	if err != nil {
		return nil, fmt.Errorf("contacts.upsert: %w", err)
	}
	body, _ := json.Marshal(map[string]any{"properties": properties})
	if existing != nil {
		raw, err := c.do(http.MethodPatch, "/crm/v3/objects/contacts/"+url.PathEscape(existing.ID), body)
		if err != nil {
			return nil, fmt.Errorf("contacts.upsert: %w", err)
		}
		var r rawContact
		json.Unmarshal(raw, &r)
		return &Contact{ID: r.ID, Email: r.Properties["email"], Properties: r.Properties}, nil
	}
	raw, err := c.do(http.MethodPost, "/crm/v3/objects/contacts", body)
	if err != nil {
		return nil, fmt.Errorf("contacts.upsert: %w", err)
	}
	var r rawContact
	json.Unmarshal(raw, &r)
	return &Contact{ID: r.ID, Email: r.Properties["email"], Properties: r.Properties}, nil
}

func truncate(b []byte) string {
	const max = 400
	s := string(b)
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}
