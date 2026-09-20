// `nanobots webhook` — where to post to fire a webhook swarm.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/redbotster/nanobots/internal/api"
	"github.com/redbotster/nanobots/internal/oneclaw"
	"github.com/redbotster/nanobots/internal/schema"
)

// runWebhook prints where to post to fire a swarm.
//
// Reads the token file and the swarm directly rather than asking a running
// daemon: the most likely moment to want this is while setting the thing
// up, and needing `nanobots up` in another terminal first would be a
// pointless dependency. The address is therefore a flag rather than
// something discovered — this cannot know what host a form service will
// reach you on.
func runWebhook(args []string) error {
	if err := rejectUnknown("webhook", args, "--addr"); err != nil {
		return err
	}
	addr := "127.0.0.1:7474"
	var name string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--addr":
			i++
			if i >= len(args) {
				return fmt.Errorf("--addr requires a host:port")
			}
			addr = args[i]
		default:
			if !strings.HasPrefix(args[i], "-") && name == "" {
				name = args[i]
			}
		}
	}
	if name == "" {
		return fmt.Errorf("usage: nanobots webhook <swarm> [--addr host:port]")
	}
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	swarmPath, sw, err := findSwarmByName(filepath.Join(root, "examples", "swarms"), name)
	if err != nil {
		return err
	}
	if !strings.EqualFold(sw.Spec.Trigger.Type, "webhook") {
		return fmt.Errorf("%s has trigger type %q, not webhook — nothing would post to this URL.\n"+
			"Add `trigger: {type: webhook}` to %s if you meant this",
			name, sw.Spec.Trigger.Type, swarmPath)
	}
	stateDir, err := oneclaw.DefaultStateDir()
	if err != nil {
		return err
	}
	token, err := api.LoadWebhookToken(stateDir)
	if err != nil {
		return err
	}
	url := "http://" + addr + "/webhooks/" + sw.Metadata.Name
	fmt.Printf("%s\n\n", url)
	fmt.Printf("curl -X POST %s \\\n  -H 'Authorization: Bearer %s' \\\n"+
		"  -H 'Content-Type: application/json' \\\n  -d '{\"example\": \"payload\"}'\n\n",
		url, token)
	fmt.Println("The body arrives as {{trigger.payload}} in this swarm's input templates.")
	fmt.Println("Anyone with that token can start this swarm, and runs send mail — treat it as a password.")
	return nil
}

// findSwarmByName resolves a swarm by its metadata name or its filename,
// the same two ways the webhook endpoint accepts.
func findSwarmByName(dir, name string) (string, *schema.Nanoswarm, error) {
	var foundPath string
	var found *schema.Nanoswarm
	err := schema.ForEachSwarmFile(dir, func(path string, sw *schema.Nanoswarm) bool {
		if sw.Metadata.Name == name || strings.TrimSuffix(filepath.Base(path), ".yaml") == name {
			foundPath, found = path, sw
			return false
		}
		return true
	})
	if err != nil {
		return "", nil, err
	}
	if found == nil {
		return "", nil, fmt.Errorf("no swarm called %q in %s", name, dir)
	}
	return foundPath, found, nil
}
