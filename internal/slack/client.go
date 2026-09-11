// Package slack is a minimal, real Slack Web API client for exactly the one
// op this build's bots need: posting a message. Unlike internal/google,
// Slack bot tokens (xoxb-...) don't expire and need no OAuth dance to use —
// a workspace admin creates one in api.slack.com/apps once and pastes it in
// (the WebUI's Settings page, or `nanobots connect slack`), and it's stored
// as a 1Claw vault secret exactly like Google's refresh token, never on
// local disk or inside a bot container.
package slack

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

var apiBase = "https://slack.com/api"

type Client struct {
	HTTPClient *http.Client
	Token      string
}

func NewClient(token string) *Client {
	return &Client{HTTPClient: &http.Client{Timeout: 15 * time.Second}, Token: token}
}

type postMessageResponse struct {
	OK      bool   `json:"ok"`
	TS      string `json:"ts"`
	Channel string `json:"channel"`
	Error   string `json:"error"`
}

// PostMessage implements `messages.post`: sends text to channel (a channel
// id like "C0123..." or a name like "#general" — Slack's API accepts both).
// Returns the posted message's timestamp, which Slack uses as its id.
func (c *Client) PostMessage(channel, text string) (ts string, err error) {
	body, _ := json.Marshal(map[string]string{"channel": channel, "text": text})
	req, err := http.NewRequest(http.MethodPost, apiBase+"/chat.postMessage", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("slack: chat.postMessage: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("slack: chat.postMessage failed (%d): %s", resp.StatusCode, truncate(raw))
	}
	var out postMessageResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("slack: parse chat.postMessage response: %w", err)
	}
	// Slack's API always returns HTTP 200, even on failure — "ok" in the
	// body is the real success signal, not the status code.
	if !out.OK {
		return "", fmt.Errorf("slack: chat.postMessage rejected: %s", out.Error)
	}
	return out.TS, nil
}

func truncate(b []byte) string {
	const max = 400
	s := string(b)
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}
