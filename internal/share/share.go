// Package share moves a swarm between machines.
//
// A swarm you build is stuck where you built it. The catalog is the only
// way to get one, and there is no way to hand someone the thing you just
// made — which is a strange gap in a product whose whole shape is "snap
// these bricks together".
//
// A bundle is the swarm's own YAML plus what a reader needs to know before
// running it: which catalog bots at which versions, which accounts it will
// want connected, and whether it will act on the world when it runs. That
// last part is the reason this is a bundle rather than "email them the
// file". A swarm is executable — `get-paid` emails your customers — and
// handing one over should say so before it runs, not after.
//
// Bots are named, not carried. They are catalog entries with versions, and
// shipping copies would fork them silently; a bundle that names
// `invoice-chaser@0.1.0` and finds it missing can say exactly that.
package share

import (
	"fmt"
	"sort"
	"strings"

	"github.com/redbotster/nanobots/internal/schema"
	"gopkg.in/yaml.v3"
)

// FormatVersion is the bundle format, so a future change can be detected
// rather than mis-parsed. Bumped only when a reader would get it wrong.
const FormatVersion = 1

// Bundle is a shareable swarm.
type Bundle struct {
	Format int `json:"format" yaml:"format"`
	// Swarm is the file, verbatim. Kept as text rather than re-marshalled
	// structure so comments survive — every catalog swarm's header explains
	// the choices it made, and that is most of what a reader needs.
	Swarm string `json:"swarm" yaml:"swarm"`
	Name  string `json:"name" yaml:"name"`
	// Requires is every catalog bot the swarm uses, as id@version.
	Requires []string `json:"requires" yaml:"requires"`
	// Connects names the accounts a recipient will have to connect before
	// this does anything real. Derived from the bots, not declared, so it
	// cannot be wrong about its own swarm.
	Connects []string `json:"connects,omitempty" yaml:"connects,omitempty"`
	// Acts lists what this swarm does to the outside world when it runs —
	// sends mail, posts, pays. The one thing someone should read before
	// pressing Run on a stranger's automation.
	Acts []string `json:"acts,omitempty" yaml:"acts,omitempty"`
}

// Export builds a bundle from a loaded swarm and its resolved bots.
//
// raw is the swarm file's own bytes. resolve returns the catalog bot for a
// `use:` reference, or an error — the caller owns catalog lookup, so this
// package stays free of filesystem layout.
func Export(raw []byte, sw *schema.Nanoswarm, resolve func(use string) (*schema.Nanobot, error)) (*Bundle, error) {
	if sw == nil {
		return nil, fmt.Errorf("export: no swarm")
	}
	b := &Bundle{Format: FormatVersion, Swarm: string(raw), Name: sw.Metadata.Name}

	seen := map[string]bool{}
	connects := map[string]bool{}
	acts := map[string]bool{}
	for _, ref := range sw.Spec.Bots {
		use := ref.Use
		if use == "" {
			// A `path:` bot lives only on the machine that wrote it. Say
			// so rather than exporting a reference that cannot resolve.
			return nil, fmt.Errorf("export: bot %q uses a local path, which cannot be shared —"+
				"put it in bots/ and reference it as id@version first", ref.ID)
		}
		if !seen[use] {
			seen[use] = true
			b.Requires = append(b.Requires, use)
		}
		nb, err := resolve(use)
		if err != nil || nb == nil {
			continue
		}
		for _, svc := range nb.Spec.Services {
			// A demo service needs nothing connected — that is the whole
			// point of demo mode, and listing it would make every bundle
			// look like it wants your Gmail.
			if svc.Connection != schema.ConnectionDemo && svc.Connection != "" {
				connects[svc.Provider] = true
			}
		}
		for _, w := range nb.Spec.Guardrails.WritesAllowed {
			acts[w] = true
		}
	}
	sort.Strings(b.Requires)
	b.Connects = sortedKeys(connects)
	b.Acts = sortedKeys(acts)
	return b, nil
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Marshal renders a bundle as YAML, with a header explaining what it is to
// someone who opens it in an editor rather than through the app.
func (b *Bundle) Marshal() ([]byte, error) {
	body, err := yaml.Marshal(b)
	if err != nil {
		return nil, err
	}
	var h strings.Builder
	h.WriteString("# A nanobots swarm bundle.\n#\n")
	fmt.Fprintf(&h, "# %s\n#\n", b.Name)
	h.WriteString("# Import it with:  nanobots import <this-file>\n")
	h.WriteString("#\n# It contains a swarm definition and the list of catalog bots it needs.\n")
	h.WriteString("# It contains no credentials: whoever imports it connects their own accounts.\n")
	if len(b.Acts) > 0 {
		fmt.Fprintf(&h, "#\n# WHEN RUN, THIS SWARM CAN WRITE TO: %s\n", strings.Join(b.Acts, ", "))
		h.WriteString("# Read it before running it, the same as any script someone sends you.\n")
	}
	h.WriteString("\n")
	return append([]byte(h.String()), body...), nil
}

// Parse reads a bundle.
func Parse(raw []byte) (*Bundle, error) {
	var b Bundle
	if err := yaml.Unmarshal(raw, &b); err != nil {
		return nil, fmt.Errorf("this is not a swarm bundle: %w", err)
	}
	if b.Format == 0 || b.Swarm == "" {
		// The likeliest mistake is handing `import` a plain swarm file.
		// Naming that beats "format is 0".
		return nil, fmt.Errorf("this is not a swarm bundle — if it is a plain swarm YAML, " +
			"copy it into examples/swarms/ instead")
	}
	if b.Format > FormatVersion {
		return nil, fmt.Errorf("this bundle is format %d and this build understands %d — upgrade nanobots",
			b.Format, FormatVersion)
	}
	return &b, nil
}

// Missing returns the required bots that have is not in the local catalog,
// given a lookup that reports whether an id@version exists.
//
// Checked before writing anything: a swarm that references a bot you don't
// have is not importable, and finding that out at run time — after it is
// saved and looks legitimate — is the worse order.
func (b *Bundle) Missing(has func(use string) bool) []string {
	var missing []string
	for _, use := range b.Requires {
		if !has(use) {
			missing = append(missing, use)
		}
	}
	return missing
}
