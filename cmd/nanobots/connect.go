// `nanobots connect` — the one-time OAuth round trip for a real account.
package main

import (
	"context"
	"fmt"

	"github.com/redbotster/nanobots/internal/google"
	"github.com/redbotster/nanobots/internal/oneclaw"
)

// runConnect handles `nanobots connect <service>`. Today that's just
// "google": a one-time interactive OAuth round trip (see
// internal/google.Connect) whose refresh token gets stored in 1Claw's vault,
// never on local disk — every bot with a Google service and
// `connection: oauth_native` then shares this one connected account (see
// docs/connections.md).
func runConnect(args []string) error {
	if len(args) != 1 || args[0] != "google" {
		return fmt.Errorf("usage: nanobots connect google")
	}
	clientID, err := google.LoadClientID("")
	if err != nil {
		return err
	}
	if clientID == "" {
		return fmt.Errorf("GOOGLE_OAUTH_CLIENT_ID is not set — add it to ~/.secrets/nanobots.env " +
			"(a Google Cloud \"Desktop app\" OAuth client id; see docs/connections.md)")
	}
	apiKey, err := oneclaw.LoadAPIKey("")
	if err != nil {
		return err
	}
	oc := oneclaw.NewClient(apiKey)
	if !oc.Configured() {
		return fmt.Errorf("ONECLAW_API_KEY is not set — the connected account's refresh token needs a 1Claw vault to live in")
	}

	fmt.Println("Opening your browser to sign in to Google — grant access, then come back here.")
	tr, err := google.Connect(context.Background(), clientID, google.DefaultScopes)
	if err != nil {
		return err
	}
	if tr.RefreshToken == "" {
		return fmt.Errorf("google did not return a refresh token — try again (this can happen if consent wasn't re-prompted)")
	}

	vault, err := oc.EnsureVault("nanobots-main")
	if err != nil {
		return fmt.Errorf("ensure 1Claw vault: %w", err)
	}
	if err := oc.PutSecret(vault.ID, "google/refresh_token", tr.RefreshToken); err != nil {
		return fmt.Errorf("store refresh token in 1Claw vault: %w", err)
	}
	fmt.Println("Connected. Gmail/Drive/Sheets bots with connection: oauth_native can now run live " +
		"(restart nanobotd if it's already running).")
	return nil
}
