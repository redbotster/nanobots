// Package oauth2pkce is a small, provider-agnostic OAuth2 authorization-code
// + PKCE client for "installed app" flows: no web server, no client secret
// required (though Config.ClientSecret is there for the providers that
// insist on one anyway — see the package doc on Config). internal/google
// has its own, older, hand-rolled copy of this same logic predating this
// package; internal/x and internal/linkedin build on this one instead of
// duplicating it a second and third time.
package oauth2pkce

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
)

// PKCE is one authorization attempt's verifier/challenge/state triple.
// Generate a fresh one per Connect call — never reuse across attempts.
type PKCE struct {
	Verifier  string
	Challenge string
	State     string
}

func NewPKCE() (*PKCE, error) {
	verifier, err := randomURLSafeString(64)
	if err != nil {
		return nil, err
	}
	state, err := randomURLSafeString(32)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	return &PKCE{Verifier: verifier, Challenge: challenge, State: state}, nil
}

func randomURLSafeString(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
