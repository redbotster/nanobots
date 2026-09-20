// A dead-simple way to turn a bot's own service from demo data to a real
// connected account, without hand-editing its nanobot.yaml — the gap this
// closes: connecting an account in Settings never changed what any bot
// actually does (every bot ships on connection: demo), and until now there
// was no UI at all for the second half of that story.
package api

import (
	"fmt"
	"net/http"
	"os"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/redbotster/nanobots/internal/schema"
)

// liveConnectionFor names the one "real" ConnectionMethod this toggle
// switches a service to — the catalog's own richer vocabulary (oauth_1claw,
// browser, ...) stays reachable only by hand-editing a nanobot.yaml
// directly, since nothing in this build wires those for any shipped bot
// yet (see docs/connections.md). Kept in sync with vaultKeyFor's provider
// set in connections.go — every provider a human can connect from Settings
// should have an entry here too.
var liveConnectionFor = map[string]schema.ConnectionMethod{
	"google":   schema.ConnectionOAuthNative,
	"x":        schema.ConnectionOAuthNative,
	"linkedin": schema.ConnectionOAuthNative,
	"slack":    schema.ConnectionAPIKeyVault,
	"github":   schema.ConnectionAPIKeyVault,
	"stripe":   schema.ConnectionAPIKeyVault,
	"hubspot":  schema.ConnectionAPIKeyVault,
}

type setBotServiceConnectionRequest struct {
	Live bool `json:"live"`
}

// handleSetBotServiceConnection flips one service on one catalog bot
// between connection: demo and its provider's real connection method —
// catalog-wide (it edits bots/<id>/nanobot.yaml itself), not scoped to one
// swarm, since that's what the file actually is. Refuses to go live unless
// the account is genuinely connected already (checked the same way
// handleConnectionsStatus does), so the WebUI's expected flow is: connect
// the account from Settings (or via the same OAuth/token action inline),
// then flip this toggle — never the other way around.
func (s *Server) handleSetBotServiceConnection(w http.ResponseWriter, r *http.Request) {
	botID := r.PathValue("id")
	serviceID := r.PathValue("serviceId")

	var req setBotServiceConnectionRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	// Not filepath.Join(s.BotsDir, botID, ...): botID is a decoded path
	// segment and "..%2f" reaches this handler as "../". See botpath.go.
	botPath, err := s.catalogBotManifest(botID)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	nb, err := schema.LoadNanobot(botPath)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	var svc *schema.Service
	for i := range nb.Spec.Services {
		if nb.Spec.Services[i].ID == serviceID {
			svc = &nb.Spec.Services[i]
			break
		}
	}
	if svc == nil {
		writeError(w, http.StatusNotFound, fmt.Errorf("bot %q has no service %q", botID, serviceID))
		return
	}

	newConnection := schema.ConnectionDemo
	if req.Live {
		live, ok := liveConnectionFor[svc.Provider]
		if !ok {
			writeError(w, http.StatusBadRequest, fmt.Errorf("no live connection method known for provider %q", svc.Provider))
			return
		}
		if err := s.requireConnectedAccount(svc.Provider); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		newConnection = live
	}

	raw, err := os.ReadFile(botPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	updated, err := setServiceConnectionInYAML(raw, serviceID, string(newConnection))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	var check schema.Nanobot
	if err := yaml.Unmarshal(updated, &check); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("editing produced invalid YAML: %w", err))
		return
	}
	if err := os.WriteFile(botPath, updated, 0o644); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	// Every swarm using this bot has a live/total count that just changed.
	s.swarmInspectCache.invalidate()

	bots, err := s.listBotSummaries()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	for _, b := range bots {
		if b.ID == botID {
			writeJSON(w, http.StatusOK, b)
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// setServiceConnectionInYAML replaces exactly one service's connection:
// value in place, leaving every other line — including comments — byte for
// byte untouched. A full struct-marshal round trip would work but would
// silently discard every explanatory comment threaded through
// bots/*/nanobot.yaml, several of which document a real, load-bearing
// design decision (see e.g. post-publisher's, receipt-filer's).
func setServiceConnectionInYAML(raw []byte, serviceID, newConnection string) ([]byte, error) {
	lines := strings.Split(string(raw), "\n")
	itemIndent := -1
	found := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if itemIndent == -1 {
			if trimmed == "- id: "+serviceID {
				itemIndent = len(line) - len(strings.TrimLeft(line, " "))
			}
			continue
		}
		lineIndent := len(line) - len(strings.TrimLeft(line, " "))
		if trimmed != "" && lineIndent <= itemIndent {
			break // ran past the end of this service's own block
		}
		if strings.HasPrefix(trimmed, "connection:") {
			indent := line[:len(line)-len(strings.TrimLeft(line, " "))]
			lines[i] = indent + "connection: " + newConnection
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("service %q has no connection: field to update", serviceID)
	}
	return []byte(strings.Join(lines, "\n")), nil
}
