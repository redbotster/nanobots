// Package roles is the perspectives a review team can be built from.
//
// A role is deliberately not a bot. One `reviewer` becomes a security
// engineer or a designer depending on what it is handed, which is why a
// review team is a swarm shape rather than eight near-identical bots to
// maintain (docs/supervisors.md).
//
// What this package adds is that the perspectives are named, listed, and
// yours. `review-board` used to invent them fresh from a prompt on every
// run: good choices, but unrepeatable, invisible before the run, and with
// nowhere to record what *your* security reviewer actually cares about.
//
// The shipped roster lives in roles/roles.yaml so a fresh clone has one.
// Edits live in ~/.nanobots/state/roles.json, never in the repo file, so a
// `git pull` doesn't fight a user's changes and every role can always say
// what it shipped with — the same shipped-versus-tuned shape the Fleet
// already uses for bot instructions.
package roles

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// Role is one perspective a reviewer can be asked to take.
type Role struct {
	ID   string `json:"id" yaml:"id"`
	Name string `json:"name" yaml:"name"`
	// Focus is the single line the reviewer is given about its own
	// perspective. It is the whole content of a role — kept to one line on
	// purpose, because a roster of blurry overlapping perspectives is how
	// review theatre starts, and the board is told that two roles which
	// would raise the same concern is one role too many.
	Focus string `json:"focus" yaml:"focus"`

	// Shipped is what roles/roles.yaml says, when the live value differs.
	// Empty for a role the user added, which has nothing to go back to.
	Shipped string `json:"shipped,omitempty" yaml:"-"`
	// Custom marks a role that exists only in the user's overrides.
	Custom bool `json:"custom,omitempty" yaml:"-"`
	// Always marks a role every review team must include.
	//
	// The board is deliberately left to choose its own team per run —
	// pinning the same three reviewers to everything is how review theatre
	// starts — but "whatever else you pick, always have security look at
	// this" is a real thing to want, and the only way to express it was to
	// stop using the board. One flag, and the board is told rather than
	// overridden.
	Always bool `json:"always,omitempty" yaml:"-"`
	// Retired marks a shipped role the user has switched off. Kept rather
	// than deleted so it can be switched back on, and so an upgrade that
	// changes its focus doesn't quietly resurrect it.
	Retired bool `json:"retired,omitempty" yaml:"-"`
}

// override is one user edit, stored separately from the shipped catalog.
type override struct {
	Name    string `json:"name,omitempty"`
	Focus   string `json:"focus,omitempty"`
	Retired bool   `json:"retired,omitempty"`
	Always  bool   `json:"always,omitempty"`
	// Added distinguishes "a role you wrote" from "an edit to a shipped
	// one", which decides whether Reset can put anything back.
	Added bool `json:"added,omitempty"`
}

// Store is the shipped catalog plus the user's edits.
type Store struct {
	// CatalogPath is roles/roles.yaml in the repo. Missing is not an
	// error: a deployment with no catalog and no overrides simply has no
	// roster, and review-board falls back to inventing roles as before.
	CatalogPath string
	// OverridePath is ~/.nanobots/state/roles.json.
	OverridePath string

	mu sync.Mutex
}

type catalogFile struct {
	Roles []Role `yaml:"roles"`
}

const maxFocus = 500

var idPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

func (s *Store) shipped() ([]Role, error) {
	if s.CatalogPath == "" {
		return nil, nil
	}
	raw, err := os.ReadFile(s.CatalogPath)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var f catalogFile
	if err := yaml.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("read role catalog %s: %w", s.CatalogPath, err)
	}
	for i := range f.Roles {
		f.Roles[i].Focus = strings.TrimSpace(f.Roles[i].Focus)
	}
	return f.Roles, nil
}

func (s *Store) overrides() (map[string]override, error) {
	out := map[string]override{}
	if s.OverridePath == "" {
		return out, nil
	}
	raw, err := os.ReadFile(s.OverridePath)
	if os.IsNotExist(err) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		// A corrupt overrides file must not make every role look untouched
		// forever, but must not be silently replaced either.
		return map[string]override{}, err
	}
	return out, nil
}

func (s *Store) writeOverrides(m map[string]override) error {
	if s.OverridePath == "" {
		return fmt.Errorf("no override path configured")
	}
	if err := os.MkdirAll(filepath.Dir(s.OverridePath), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	// Temp-then-rename: a half-written file would lose every edit, not
	// just the one being made.
	tmp, err := os.CreateTemp(filepath.Dir(s.OverridePath), ".roles-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), s.OverridePath)
}

// List returns every role — shipped, edited and added — in a stable order:
// the catalog's own order first, then anything the user added,
// alphabetically. Retired roles are included and marked, because a UI that
// hid them would leave no way to switch one back on.
func (s *Store) List() ([]Role, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.list()
}

func (s *Store) list() ([]Role, error) {
	shipped, shipErr := s.shipped()
	ov, ovErr := s.overrides()

	var out []Role
	seen := map[string]bool{}
	for _, r := range shipped {
		seen[r.ID] = true
		role := r
		if o, ok := ov[r.ID]; ok {
			if o.Name != "" {
				role.Name = o.Name
			}
			if o.Focus != "" {
				role.Focus = o.Focus
			}
			role.Retired = o.Retired
			role.Always = o.Always
			if role.Focus != r.Focus {
				role.Shipped = r.Focus
			}
		}
		out = append(out, role)
	}

	var added []Role
	for id, o := range ov {
		if seen[id] {
			continue
		}
		added = append(added, Role{
			ID: id, Name: o.Name, Focus: o.Focus, Custom: true, Retired: o.Retired, Always: o.Always,
		})
	}
	sort.Slice(added, func(i, j int) bool { return added[i].ID < added[j].ID })
	out = append(out, added...)

	if shipErr != nil {
		return out, shipErr
	}
	return out, ovErr
}

// Active is the roles a review board may actually pick from.
func (s *Store) Active() ([]Role, error) {
	all, err := s.List()
	active := make([]Role, 0, len(all))
	for _, r := range all {
		if !r.Retired && strings.TrimSpace(r.Focus) != "" {
			active = append(active, r)
		}
	}
	return active, err
}

// Roster renders the active roles as the text a review board is given.
//
// Plain lines rather than JSON: this goes into a prompt, and a model
// reading "Security engineer — credentials, secrets and data exposure"
// picks better than one parsing an object. The name before the dash is
// what the board must echo back, and the prompt says so.
func (s *Store) Roster() (string, error) {
	active, err := s.Active()
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, r := range active {
		// The marker is in the line rather than in a separate list so the
		// board reads it exactly where it reads the role.
		always := ""
		if r.Always {
			always = " [ALWAYS INCLUDE]"
		}
		fmt.Fprintf(&b, "- %s%s — %s\n", r.Name, always, collapse(r.Focus))
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

// collapse turns a folded YAML block into one line, since the roster is
// line-oriented and a role whose focus wrapped would look like several.
func collapse(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// Set edits a role, or adds one that isn't in the catalog.
func (s *Store) Set(id, name, focus string) error {
	id = strings.TrimSpace(id)
	if !idPattern.MatchString(id) {
		return fmt.Errorf("role id %q must be lowercase words joined by hyphens (e.g. \"security\", \"staff-engineer\")", id)
	}
	name = strings.TrimSpace(name)
	focus = collapse(focus)
	if name == "" {
		return fmt.Errorf("a role needs a name")
	}
	if focus == "" {
		return fmt.Errorf("a role needs a focus — the one line telling its reviewer what to look at")
	}
	if len(focus) > maxFocus {
		return fmt.Errorf("focus is %d characters; keep it under %d — it is one line in a prompt, not a brief", len(focus), maxFocus)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	ov, _ := s.overrides()
	shipped, err := s.shipped()
	if err != nil {
		return err
	}
	known := false
	for _, r := range shipped {
		if r.ID == id {
			known = true
		}
	}
	prev := ov[id]
	ov[id] = override{Name: name, Focus: focus, Retired: prev.Retired, Always: prev.Always, Added: prev.Added || !known}
	return s.writeOverrides(ov)
}

// SetAlways marks a role as one every review team must include.
func (s *Store) SetAlways(id string, always bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ov, _ := s.overrides()
	o := ov[id]
	o.Always = always
	ov[id] = o
	return s.writeOverrides(ov)
}

// SetRetired switches a role off or back on without losing its wording.
func (s *Store) SetRetired(id string, retired bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ov, _ := s.overrides()
	o := ov[id]
	o.Retired = retired
	ov[id] = o
	return s.writeOverrides(ov)
}

// Reset drops a role's override: a shipped role goes back to what it
// shipped with, and one the user added is removed entirely.
func (s *Store) Reset(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ov, _ := s.overrides()
	if _, ok := ov[id]; !ok {
		return nil
	}
	delete(ov, id)
	return s.writeOverrides(ov)
}
