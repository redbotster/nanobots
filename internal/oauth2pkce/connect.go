package oauth2pkce

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"time"
)

// Connect runs one full interactive OAuth round trip: generates PKCE,
// starts the loopback redirect catcher, opens the system browser to the
// provider's real consent screen (no automation, no CDP), waits for the
// human to approve, and exchanges the resulting code for tokens.
func Connect(ctx context.Context, cfg Config) (*TokenResponse, error) {
	if cfg.ClientID == "" {
		return nil, fmt.Errorf("oauth2pkce: no client id configured")
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

	authURL := AuthURL(cfg, loopback.RedirectURI(), pkce)
	if err := OpenBrowser(authURL); err != nil {
		return nil, fmt.Errorf("oauth2pkce: open browser for %s: %w", authURL, err)
	}

	waitCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	result, err := loopback.Wait(waitCtx)
	if err != nil {
		return nil, fmt.Errorf("oauth2pkce: timed out waiting for you to approve access: %w", err)
	}
	if result.Err != nil {
		return nil, result.Err
	}
	if result.State != pkce.State {
		return nil, fmt.Errorf("oauth2pkce: state mismatch — the redirect didn't match this attempt (possible CSRF)")
	}
	return ExchangeCode(cfg, result.Code, pkce.Verifier, loopback.RedirectURI())
}

func OpenBrowser(url string) error {
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
