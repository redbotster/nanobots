package google

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// TokenSource returns a currently-valid access token, refreshing behind the
// scenes as needed. internal/step.LiveDeps supplies one backed by a refresh
// token read from the 1Claw vault — this package never stores or refreshes
// a token on its own, it just uses whatever it's handed.
type TokenSource func() (string, error)

// Client is a small, real REST client for exactly the Gmail/Drive/Sheets
// ops this build's bots declare — not a general API wrapper. Each method
// name matches a nanobot.yaml `op:` value 1:1 (see docs/connections.md).
type Client struct {
	HTTPClient *http.Client
	Token      TokenSource
}

func NewClient(token TokenSource) *Client {
	return &Client{HTTPClient: &http.Client{Timeout: 30 * time.Second}, Token: token}
}

func (c *Client) do(method, url string, body []byte, contentType string) ([]byte, error) {
	token, err := c.Token()
	if err != nil {
		return nil, fmt.Errorf("google: get access token: %w", err)
	}
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("google: %s %s: %w", method, url, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("google: %s %s failed (%d): %s", method, url, resp.StatusCode, truncate(raw))
	}
	return raw, nil
}

func (c *Client) getJSON(url string, out any) error {
	raw, err := c.do(http.MethodGet, url, nil, "")
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}

func (c *Client) postJSON(url string, in, out any) error {
	body, err := json.Marshal(in)
	if err != nil {
		return err
	}
	raw, err := c.do(http.MethodPost, url, body, "application/json")
	if err != nil {
		return err
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, out)
}
