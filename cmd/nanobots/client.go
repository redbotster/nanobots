// The 1Claw client the account-facing commands share.
package main

import (
	"fmt"

	"github.com/redbotster/nanobots/internal/oneclaw"
)

// oneClawClient builds a configured client or explains what's missing.
func oneClawClient() (*oneclaw.Client, error) {
	apiKey, err := oneclaw.LoadAPIKey("")
	if err != nil {
		return nil, err
	}
	oc := oneclaw.NewClient(apiKey)
	if !oc.Configured() {
		return nil, fmt.Errorf("ONECLAW_API_KEY is not set in ~/.secrets/nanobots.env")
	}
	return oc, nil
}
