package google

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"time"
)

// Connect runs one full interactive OAuth round trip: generates PKCE,
// starts the loopback redirect catcher, opens the system browser to
// Google's real consent screen (no automation, no CDP — see the package
// doc comment for why that matters here specifically), waits for the human
// to approve, and exchanges the resulting code for tokens.
//
// Returns the refresh token on success — callers persist that (see
// docs/oneclaw-bridge.md's pattern: a 1Claw vault secret, never local disk)
// and use RefreshAccessToken for every actual Gmail/Drive call afterward.
func Connect(ctx context.Context, clientID string, scopes []string) (*TokenResponse, error) {
	if clientID == "" {
		return nil, fmt.Errorf("google: no client id configured (set GOOGLE_OAUTH_CLIENT_ID)")
	}
	pkce, err := NewPKCE()
	if err != nil {
		return nil, err
	}
	loopback, err := StartLoopback()
	if err != nil {
		return nil, err
	}
	defer loopback.Close()

	authURL := AuthURL(clientID, loopback.RedirectURI(), scopes, pkce)
	if err := openBrowser(authURL); err != nil {
		return nil, fmt.Errorf("google: open browser for %s: %w", authURL, err)
	}

	waitCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	result, err := loopback.Wait(waitCtx)
	if err != nil {
		return nil, fmt.Errorf("google: timed out waiting for you to approve access: %w", err)
	}
	if result.Err != nil {
		return nil, result.Err
	}
	if result.State != pkce.State {
		return nil, fmt.Errorf("google: state mismatch — the redirect didn't match this attempt (possible CSRF)")
	}
	return ExchangeCode(clientID, result.Code, pkce.Verifier, loopback.RedirectURI())
}

func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		return fmt.Errorf("don't know how to open a browser on %s — open manually: %s", runtime.GOOS, url)
	}
	return cmd.Start()
}
