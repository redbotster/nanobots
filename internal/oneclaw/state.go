package oneclaw

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// agentCredential is what gets persisted per named agent — never committed
// to the repo (it lives under ~/.nanobots/state by default, outside any git
// tree) and written with 0600 permissions.
type agentCredential struct {
	AgentID string `json:"agent_id"`
	APIKey  string `json:"api_key"`
}

// DefaultStateDir returns ~/.nanobots/state, creating it if needed.
func DefaultStateDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".nanobots", "state", "agents")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

func credentialPath(stateDir, name string) string {
	return filepath.Join(stateDir, name+".json")
}

func loadAgentCredential(stateDir, name string) (agentCredential, bool, error) {
	raw, err := os.ReadFile(credentialPath(stateDir, name))
	if err != nil {
		if os.IsNotExist(err) {
			return agentCredential{}, false, nil
		}
		return agentCredential{}, false, err
	}
	var cred agentCredential
	if err := json.Unmarshal(raw, &cred); err != nil {
		return agentCredential{}, false, fmt.Errorf("parse credential file %s: %w", credentialPath(stateDir, name), err)
	}
	return cred, true, nil
}

func saveAgentCredential(stateDir, name string, cred agentCredential) error {
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return err
	}
	raw, err := json.Marshal(cred)
	if err != nil {
		return err
	}
	return os.WriteFile(credentialPath(stateDir, name), raw, 0o600)
}
