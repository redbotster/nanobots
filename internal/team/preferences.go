package team

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Preferences is which engine a delegation actually uses — the global
// default, plus any per-role override — live-updatable from Settings with
// no daemon restart.
//
// Before this, the engine was `lab.Config.DefaultEngine`: a plain field,
// resolved once at daemon startup from whichever of ANTHROPIC_API_KEY /
// GEMINI_API_KEY was present, Claude preferred if both. There was no way
// to see which one would answer, or to change it, short of editing the
// env file and restarting. Found live: a real request silently ran on a
// Gemini free-tier key with a five-to-twenty-request quota and burned
// through it before failing, with nothing in the app saying it would.
//
// A pointer to one instance is shared between the API's write handlers
// (internal/api/labengines.go) and Lab's own delegate() call, so a change
// here reaches the very next message, not just the next boot.
type Preferences struct {
	// Path is where this persists — a plain JSON file next to the other
	// small state this daemon owns, the same reasoning and the same
	// override-file shape as internal/roles.Store.
	Path string

	mu    sync.Mutex
	def   Engine
	roles map[string]Engine
}

type preferencesFile struct {
	Default Engine            `json:"default_engine,omitempty"`
	Roles   map[string]Engine `json:"role_engines,omitempty"`
}

// NewPreferences loads path if it exists. fallbackDefault is whatever the
// daemon resolved from which API key is actually present — a file that
// has never been written yet must not silently mean "no engine at all"
// for a role that already worked before this existed.
func NewPreferences(path string, fallbackDefault Engine) (*Preferences, error) {
	p := &Preferences{Path: path, def: fallbackDefault, roles: map[string]Engine{}}
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return p, nil
	}
	if err != nil {
		return nil, err
	}
	var f preferencesFile
	if err := json.Unmarshal(raw, &f); err != nil {
		// A corrupt file must not take Lab's delegation down with it — start
		// clean on the fallback rather than fail the whole daemon over one
		// small state file.
		return &Preferences{Path: path, def: fallbackDefault, roles: map[string]Engine{}}, nil
	}
	if f.Default != "" {
		p.def = f.Default
	}
	if f.Roles != nil {
		p.roles = f.Roles
	}
	return p, nil
}

// EngineFor returns which engine role should use right now: its own
// override if it has one, otherwise the default.
func (p *Preferences) EngineFor(role string) Engine {
	p.mu.Lock()
	defer p.mu.Unlock()
	if e, ok := p.roles[role]; ok && e != "" {
		return e
	}
	return p.def
}

// Default returns the engine used by any role with no override of its own
// — "" means no engine is configured at all (neither key present).
func (p *Preferences) Default() Engine {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.def
}

// RoleOverrides returns a snapshot of every role that has one, keyed by
// role name — the roles this returns nothing for still delegate, just to
// Default() instead.
func (p *Preferences) RoleOverrides() map[string]Engine {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make(map[string]Engine, len(p.roles))
	for k, v := range p.roles {
		out[k] = v
	}
	return out
}

func validEngine(e Engine) error {
	if e != EngineClaude && e != EngineGemini {
		return fmt.Errorf("unknown engine %q — must be %q or %q", e, EngineClaude, EngineGemini)
	}
	return nil
}

// SetDefault changes the engine used by any role with no override of its
// own, and persists it. Takes effect on Lab's very next delegation.
func (p *Preferences) SetDefault(e Engine) error {
	if err := validEngine(e); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	orig := p.def
	p.def = e
	if err := p.save(); err != nil {
		p.def = orig
		return err
	}
	return nil
}

// SetRoleEngine overrides one role's engine; e == "" clears the override,
// falling back to Default() again.
func (p *Preferences) SetRoleEngine(role string, e Engine) error {
	if e != "" {
		if err := validEngine(e); err != nil {
			return err
		}
	}
	if role == "" {
		return fmt.Errorf("a role name is required")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	orig, had := p.roles[role]
	if e == "" {
		delete(p.roles, role)
	} else {
		p.roles[role] = e
	}
	if err := p.save(); err != nil {
		if had {
			p.roles[role] = orig
		} else {
			delete(p.roles, role)
		}
		return err
	}
	return nil
}

// save must be called with mu held.
func (p *Preferences) save() error {
	if p.Path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(p.Path), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(preferencesFile{Default: p.def, Roles: p.roles}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p.Path, raw, 0o600)
}
