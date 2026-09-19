package step

import (
	"fmt"

	"github.com/redbotster/nanobots/internal/hubspot"
)

// HubSpotConfig configures direct HubSpot access for a service with
// provider: hubspot and a non-demo connection. TokenKey defaults to
// "hubspot/token" in the same 1Claw vault every other direct integration
// uses.
type HubSpotConfig struct {
	TokenKey string
}

func (cfg HubSpotConfig) tokenConfig() VaultTokenConfig {
	key := cfg.TokenKey
	if key == "" {
		key = "hubspot/token"
	}
	return VaultTokenConfig{Key: key}
}

// hubspotAPI is the subset of *hubspot.Client's methods dispatchHubSpot calls.
type hubspotAPI interface {
	ContactSearch(email string) (*hubspot.Contact, error)
	ContactUpsert(properties map[string]string) (*hubspot.Contact, error)
}

func (l *LiveDeps) hubspotClient() (hubspotAPI, error) {
	cfg := l.Services.HubSpot.tokenConfig()
	if l.hubspotTokenCache == nil {
		l.hubspotTokenCache = &vaultToken{store: l.Secrets, cfg: cfg}
	}
	token, err := l.hubspotTokenCache.Get()
	if err != nil {
		return nil, wrapTokenErr("hubspot", "connect a private-app token from Settings", err)
	}
	return hubspot.NewClient(token), nil
}

// dispatchHubSpot maps a service.call op onto the real client. "contacts.upsert"
// takes params as the whole properties map (must include "email") rather than
// a nested wrapper — a bot's nanobot.yaml passes `params: { email: ..., ... }`
// directly.
func dispatchHubSpot(c hubspotAPI, op string, params map[string]any) (any, error) {
	switch op {
	case "contacts.search":
		email, _ := params["email"].(string)
		contact, err := c.ContactSearch(email)
		if err != nil {
			return nil, err
		}
		if contact == nil {
			return nil, nil
		}
		return toJSONAny(contact)
	case "contacts.upsert":
		props := make(map[string]string, len(params))
		for k, v := range params {
			if s, ok := v.(string); ok {
				props[k] = s
			}
		}
		contact, err := c.ContactUpsert(props)
		if err != nil {
			return nil, err
		}
		return toJSONAny(contact)
	default:
		return nil, fmt.Errorf("hubspot: unsupported op %q", op)
	}
}
