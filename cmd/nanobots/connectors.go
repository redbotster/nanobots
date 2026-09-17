// `nanobots connectors` — register an OAuth app once with 1Claw and wire
// it to a bot.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/redbotster/nanobots/internal/oneclaw"
	"github.com/redbotster/nanobots/internal/runner"
	"github.com/redbotster/nanobots/internal/schema"
)

// runConnectors uses 1Claw's own reviewed OAuth applications instead of
// making you register your own.
//
// This is the wall the project has had all along: pointing a bot at a real
// account meant a Google Cloud project, a consent screen and a client id,
// and thirteen of the catalog's bots are Google bots that stayed on demo
// fixtures until someone did that. 1Claw already holds apps for Gmail,
// Calendar, Sheets, Slack, GitHub and X. One install, one browser round
// trip, done.
func runConnectors(args []string) error {
	if err := rejectUnknown("connectors", args); err != nil {
		return err
	}
	oc, err := oneClawClient()
	if err != nil {
		return err
	}
	action := "list"
	if len(args) > 0 {
		action = args[0]
	}

	switch action {
	case "list":
		presets, err := oc.ListConnectorPresets()
		if err != nil {
			return err
		}
		fmt.Printf("%d connectors available through 1Claw — no provider console needed:\n\n", len(presets))
		for _, p := range presets {
			name := p.DisplayName
			if name == "" {
				name = p.Slug
			}
			fmt.Printf("  %-16s %s\n", p.Slug, name)
		}
		fmt.Println("\nnanobots connectors install <bot> <slug>   e.g. inbox-triage gmail")
		return nil

	case "install":
		if len(args) < 3 {
			return fmt.Errorf("usage: nanobots connectors install <bot-id> <connector-slug>\n" +
				"  the bot is which nanobot gets the credential (its agent), e.g. inbox-triage")
		}
		botID, slug := args[1], args[2]
		// The binding must be named after the service the bot declares, or
		// LiveDeps.ServiceCall will look for one that isn't there.
		bindingName := slug
		for i := 3; i < len(args); i++ {
			if args[i] == "--as" && i+1 < len(args) {
				i++
				bindingName = args[i]
			}
		}

		agentID, err := agentForBot(oc, botID)
		if err != nil {
			return err
		}

		got, err := oc.InstallConnector(agentID, slug, bindingName, nil)
		if err != nil {
			return err
		}
		fmt.Printf("installed %s on %s as binding %q\n", got.PresetSlug, botID, got.BindingName)
		if got.NextStep != "" {
			fmt.Printf("\n%s\n", got.NextStep)
		}
		if got.AuthorizationURL != "" {
			fmt.Printf("\nOpen this to grant access:\n  %s\n", got.AuthorizationURL)
		}
		fmt.Printf("\nThen set the bot's service to connection: oauth_1claw and it will use the real account.\n")
		return nil

	case "register":
		// The step every OAuth connector needs first. 1Claw does not ship
		// shared OAuth apps: an install answers "No app credentials
		// configured" until this has been done for that provider.
		if len(args) < 5 {
			return fmt.Errorf("usage: nanobots connectors register <bot-id> <provider> <client-id> <client-secret>\n" +
				"  providers: google slack github x notion discord hubspot linkedin microsoft salesforce\n" +
				"  the secret goes straight to 1Claw and is never written to disk here")
		}
		agentID, err := agentForBot(oc, args[1])
		if err != nil {
			return err
		}
		if err := oc.SaveOAuthAppCredentials(agentID, args[2], args[3], args[4], ""); err != nil {
			return err
		}
		fmt.Printf("registered a %s OAuth app for %s\n", args[2], args[1])
		fmt.Printf("now: nanobots connectors install %s <slug>\n", args[1])
		return nil

	case "status":
		if len(args) < 2 {
			return fmt.Errorf("usage: nanobots connectors status <bot-id>")
		}
		agentID, err := agentForBot(oc, args[1])
		if err != nil {
			return err
		}
		if creds, err := oc.ListOAuthAppCredentials(agentID); err == nil && len(creds) > 0 {
			for _, c := range creds {
				fmt.Printf("  oauth app        %s registered\n", c.Provider)
			}
		}
		list, err := oc.ListAgentConnectors(agentID)
		if err != nil {
			return err
		}
		if len(list) == 0 {
			fmt.Printf("%s has no connectors installed\n", args[1])
			return nil
		}
		for _, c := range list {
			state := "installed, not yet authorised"
			if c.Connected {
				state = "connected"
			}
			fmt.Printf("  %-16s %s\n", c.BindingName, state)
		}
		return nil
	}
	return fmt.Errorf("usage: nanobots connectors list|install|status")
}

// agentForBot resolves (creating if needed) the 1Claw agent a bot's
// credentials hang off. A connector is installed on an agent, and this
// build gives each bot its own.
func agentForBot(oc *oneclaw.Client, botID string) (string, error) {
	root, err := os.Getwd()
	if err != nil {
		return "", err
	}
	nb, err := schema.LoadNanobot(filepath.Join(root, "bots", botID, "nanobot.yaml"))
	if err != nil {
		return "", fmt.Errorf("no bot %q in bots/: %w", botID, err)
	}
	stateDir, err := oneclaw.DefaultStateDir()
	if err != nil {
		return "", err
	}
	// Through the same derivation the runner uses. This used to mint
	// "nanobots-"+bot name on its own, which is how `nanobots connectors
	// install` created an agent the runner would never look at again —
	// a leftover the moment it was made.
	name := runner.AgentNameFor(nb)
	if name == "" {
		return "", fmt.Errorf("bot %q needs no 1Claw agent, so there is nothing to install a connector on", botID)
	}
	id, _, err := oc.EnsureAgent(stateDir, name,
		oneclaw.CreateAgentRequest{ShroudEnabled: true, MemoryEnabled: true})
	if err != nil {
		return "", fmt.Errorf("ensure this bot's 1Claw agent: %w", err)
	}
	return id, nil
}
