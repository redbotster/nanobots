package api

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"

	"github.com/redbotster/nanobots/internal/schema"
)

// Connecting an account and having your bots actually use it were two
// separate jobs, and the second one had no bulk path.
//
// On a fresh install every one of the thirty bots ships on `connection:
// demo`, which is the right default — nothing should touch a real inbox
// until a human says so. But 24 of the 32 declared services are Google, so
// "connect Google and start using it" meant one OAuth round trip followed
// by twenty-four individual toggles hunted down across the bot library.
// Measured on this machine: 14 of 14 swarms entirely on demo data.
//
// This endpoint does the second half in one action, in both directions.

type providerBotsResponse struct {
	Provider string `json:"provider"`
	// Demo and Live are bot ids, sorted, so the UI can say "24 bots" and
	// name them rather than just claiming a number.
	Demo []string `json:"demo"`
	Live []string `json:"live"`
}

type setProviderConnectionRequest struct {
	Live bool `json:"live"`
}

type setProviderConnectionResponse struct {
	Provider string   `json:"provider"`
	Changed  []string `json:"changed"`
	// Failed maps a bot id to why it couldn't be switched. A partial
	// failure is reported rather than swallowed or rolled back: the bots
	// that did switch are genuinely switched, and pretending otherwise
	// would be worse than saying which ones didn't.
	Failed map[string]string `json:"failed,omitempty"`
}

// botsUsingProvider lists every catalog bot with a service from provider,
// split by whether it's currently on demo data or a real account.
func (s *Server) botsUsingProvider(provider string) (providerBotsResponse, error) {
	out := providerBotsResponse{Provider: provider, Demo: []string{}, Live: []string{}}
	err := schema.ForEachBotDir(s.BotsDir, func(id string, nb *schema.Nanobot) {
		for _, svc := range nb.Spec.Services {
			if svc.Provider != provider {
				continue
			}
			if svc.Connection == "" || svc.Connection == schema.ConnectionDemo {
				out.Demo = append(out.Demo, id)
			} else {
				out.Live = append(out.Live, id)
			}
			break // one entry per bot, not per service
		}
	})
	if err != nil {
		return out, err
	}
	sort.Strings(out.Demo)
	sort.Strings(out.Live)
	return out, nil
}

// handleProviderBots answers "how many bots would this affect", so the UI
// can offer the action with a real number instead of a vague promise.
func (s *Server) handleProviderBots(w http.ResponseWriter, r *http.Request) {
	resp, err := s.botsUsingProvider(r.PathValue("service"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleSetProviderConnection switches every catalog bot's services for one
// provider between demo fixtures and the connected account.
//
// Going live is gated exactly as the single-bot toggle is — the account has
// to genuinely be connected first — because this is the action that makes
// bots read a real inbox and write to a real Drive. Going back to demo is
// never gated: undoing should always be at least as easy as doing.
func (s *Server) handleSetProviderConnection(w http.ResponseWriter, r *http.Request) {
	provider := r.PathValue("service")

	var req setProviderConnectionRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	target := schema.ConnectionDemo
	if req.Live {
		live, ok := liveConnectionFor[provider]
		if !ok {
			writeError(w, http.StatusBadRequest, fmt.Errorf("no live connection method known for provider %q", provider))
			return
		}
		if err := s.requireConnectedAccount(provider); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		target = live
	}

	resp := setProviderConnectionResponse{Provider: provider, Changed: []string{}}
	err := schema.ForEachBotDir(s.BotsDir, func(id string, _ *schema.Nanobot) {
		changed, err := s.setProviderOnBot(id, provider, target)
		if err != nil {
			if resp.Failed == nil {
				resp.Failed = map[string]string{}
			}
			resp.Failed[id] = err.Error()
			return
		}
		if changed {
			resp.Changed = append(resp.Changed, id)
		}
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if len(resp.Changed) > 0 {
		// Every swarm using any of these bots has a live/total count that
		// just changed — same reasoning as the single-bot toggle.
		s.swarmInspectCache.invalidate()
	}
	sort.Strings(resp.Changed)
	writeJSON(w, http.StatusOK, resp)
}

// requireConnectedAccount is the same check handleConnectionsStatus makes,
// shared here (and by the single-bot toggle in bot_connection.go) so going
// live can never disagree with what Settings itself reports as connected —
// there must be a real credential in the right backend before any bot is
// pointed at a real account.
func (s *Server) requireConnectedAccount(provider string) error {
	if staticTokenServices[provider] {
		if s.Secrets == nil {
			return fmt.Errorf("no secrets backend configured — see docs/secrets.md")
		}
		if _, found, err := s.Secrets.Get(vaultKeyFor[provider]); err != nil {
			return err
		} else if !found {
			return fmt.Errorf("%s isn't connected yet — connect it from Settings first", provider)
		}
		return nil
	}
	if s.OneClaw == nil || !s.OneClaw.Configured() {
		return fmt.Errorf("1Claw isn't configured — connect an account from Settings first")
	}
	vault, err := s.OneClaw.EnsureVault("nanobots-main")
	if err != nil {
		return err
	}
	if s.serviceConnected(provider, vault.ID) {
		return nil
	}
	return fmt.Errorf("%s isn't connected yet — connect it from Settings first", provider)
}

// setProviderOnBot rewrites every service of one provider on one bot,
// reporting whether anything actually changed. Uses the same surgical YAML
// edit the single-bot toggle does, so each bot's comments survive.
func (s *Server) setProviderOnBot(botID, provider string, target schema.ConnectionMethod) (bool, error) {
	path := filepath.Join(s.BotsDir, botID, "nanobot.yaml")
	nb, err := schema.LoadNanobot(path)
	if err != nil {
		return false, nil // not a bot; skip quietly
	}

	var toChange []string
	for _, svc := range nb.Spec.Services {
		if svc.Provider != provider {
			continue
		}
		current := svc.Connection
		if current == "" {
			current = schema.ConnectionDemo
		}
		if current != target {
			toChange = append(toChange, svc.ID)
		}
	}
	if len(toChange) == 0 {
		return false, nil
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	for _, serviceID := range toChange {
		raw, err = setServiceConnectionInYAML(raw, serviceID, string(target))
		if err != nil {
			return false, fmt.Errorf("service %q: %w", serviceID, err)
		}
	}
	// Re-parse before writing: a surgical text edit that produced something
	// unloadable must never reach disk.
	var check schema.Nanobot
	if err := yaml.Unmarshal(raw, &check); err != nil {
		return false, fmt.Errorf("edit produced an unloadable nanobot.yaml: %w", err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		return false, err
	}
	return true, nil
}
