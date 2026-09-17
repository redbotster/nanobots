// `nanobots export` / `nanobots import` — hand a swarm to someone else,
// and take one back.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/redbotster/nanobots/internal/schema"
	"github.com/redbotster/nanobots/internal/share"
)

// runExport bundles a swarm so it can be handed to someone else.
//
// A swarm you build has been stuck on the machine that built it: the
// catalog was the only way to get one. The bundle carries the file itself,
// which bots it needs, which accounts the recipient will have to connect,
// and — the part worth surfacing — what it can write to when it runs.
func runExport(args []string) error {
	if err := rejectUnknown("export", args, "-f", "--file", "-o", "--out"); err != nil {
		return err
	}
	var swarmPath, out string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-f", "--file":
			i++
			if i >= len(args) {
				return fmt.Errorf("-f requires a swarm file")
			}
			swarmPath = args[i]
		case "-o", "--out":
			i++
			if i >= len(args) {
				return fmt.Errorf("-o requires a file to write")
			}
			out = args[i]
		}
	}
	if swarmPath == "" {
		return fmt.Errorf("usage: nanobots export -f <swarm.yaml> [-o <file>]")
	}
	raw, err := os.ReadFile(swarmPath)
	if err != nil {
		return err
	}
	sw, err := schema.LoadNanoswarm(swarmPath)
	if err != nil {
		return err
	}
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	bundle, err := share.Export(raw, sw, catalogLookup(filepath.Join(root, "bots")))
	if err != nil {
		return err
	}
	body, err := bundle.Marshal()
	if err != nil {
		return err
	}
	if out == "" {
		// To stdout, so it pipes. A bundle is text on purpose.
		fmt.Print(string(body))
		return nil
	}
	if err := os.WriteFile(out, body, 0o644); err != nil {
		return err
	}
	fmt.Printf("wrote %s — %d bot(s)", out, len(bundle.Requires))
	if len(bundle.Acts) > 0 {
		fmt.Printf(", can write to %s", strings.Join(bundle.Acts, ", "))
	}
	fmt.Println()
	return nil
}

// runImport adds a shared swarm to the local catalog.
//
// Everything is checked before anything is written. A swarm referencing a
// bot you do not have is not importable, and discovering that at run time —
// after it has been saved and looks legitimate — is the worse order.
func runImport(args []string) error {
	if err := rejectUnknown("import", args); err != nil {
		return err
	}
	if len(args) == 0 {
		return fmt.Errorf("usage: nanobots import <bundle.yaml>")
	}
	raw, err := os.ReadFile(args[0])
	if err != nil {
		return err
	}
	bundle, err := share.Parse(raw)
	if err != nil {
		return err
	}
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	botsDir := filepath.Join(root, "bots")
	missing := bundle.Missing(func(use string) bool {
		nb, err := catalogLookup(botsDir)(use)
		return err == nil && nb != nil
	})
	if len(missing) > 0 {
		return fmt.Errorf("this bundle needs bots you do not have: %s\n"+
			"nothing was written — add them to bots/ and import again", strings.Join(missing, ", "))
	}

	dest := filepath.Join(root, "examples", "swarms", slugify(bundle.Name)+".yaml")
	if _, err := os.Stat(dest); err == nil {
		return fmt.Errorf("%s already exists — rename the swarm in the bundle, or move the existing file", dest)
	}
	if err := os.WriteFile(dest, []byte(bundle.Swarm), 0o644); err != nil {
		return err
	}

	fmt.Printf("imported %s\n", dest)
	if len(bundle.Connects) > 0 {
		fmt.Printf("  it wants these accounts connected: %s\n", strings.Join(bundle.Connects, ", "))
	}
	if len(bundle.Acts) > 0 {
		fmt.Printf("  when it runs it can write to: %s\n", strings.Join(bundle.Acts, ", "))
	}
	fmt.Printf("\ncheck it before running:  nanobots plan -f %s\n", dest)
	return nil
}

// catalogLookup resolves an id@version reference against bots/.
func catalogLookup(botsDir string) func(string) (*schema.Nanobot, error) {
	return func(use string) (*schema.Nanobot, error) {
		id, _, _ := strings.Cut(use, "@")
		return schema.LoadNanobot(filepath.Join(botsDir, id, "nanobot.yaml"))
	}
}

// slugify is main's own copy of the builder's rule, so an imported swarm
// lands with the same kind of filename a saved one does.
func slugify(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	var b strings.Builder
	lastDash := true
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		out = "imported-swarm"
	}
	return out
}
