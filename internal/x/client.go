package x

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const apiBase = "https://api.x.com/2"

// Client is a real X API v2 client bound to one user's OAuth2 access token.
type Client struct {
	HTTPClient  *http.Client
	AccessToken string
	BaseURL     string // overridable for tests
}

func NewClient(accessToken string) *Client {
	return &Client{
		HTTPClient:  &http.Client{Timeout: 15 * time.Second},
		AccessToken: accessToken,
		BaseURL:     apiBase,
	}
}

// PostTweet publishes a tweet as the connected account and returns its id.
func (c *Client) PostTweet(text string) (string, error) {
	body, err := json.Marshal(map[string]string{"text": text})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequest(http.MethodPost, c.BaseURL+"/tweets", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.AccessToken)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("x: post tweet: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("x: post tweet rejected (%d): %s", resp.StatusCode, truncate(raw))
	}
	var out struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("x: parse post response: %w", err)
	}
	if out.Data.ID == "" {
		return "", fmt.Errorf("x: post response had no tweet id: %s", truncate(raw))
	}
	return out.Data.ID, nil
}

func truncate(b []byte) string {
	const max = 400
	s := string(b)
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}
