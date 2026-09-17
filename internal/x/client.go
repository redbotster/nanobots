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

// Mention is one post that mentioned the connected account.
type Mention struct {
	ID       string `json:"id"`
	Text     string `json:"text"`
	AuthorID string `json:"author_id"`
	Author   string `json:"author"` // @handle, resolved from the expansion
	Created  string `json:"created_at"`
}

// Me returns the connected account's numeric user id, which every read
// endpoint is keyed on. X has no "mentions of me" route that accepts a
// handle; it wants the id.
func (c *Client) Me() (string, error) {
	var out struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := c.get("/users/me", nil, &out); err != nil {
		return "", err
	}
	if out.Data.ID == "" {
		return "", fmt.Errorf("x: /users/me returned no id")
	}
	return out.Data.ID, nil
}

// MentionPage is what one mentions read returned: the posts, and the
// newest id X itself reports for them.
//
// NewestID is X's own `meta.newest_id`, not something computed here. A
// watch has to hand the next run a bookmark, and deriving one by sorting
// snowflake ids as strings breaks the day the id length changes — which is
// the kind of thing that fails quietly, months later, by re-reading and
// re-billing every post in the window.
type MentionPage struct {
	Mentions []Mention
	NewestID string
}

// Mentions returns recent posts mentioning the connected account, newest
// first. sinceID is exclusive and may be empty for "whatever the window
// gives you".
//
// Reads are billed. X removed the free tier in February 2026 and charges
// per post read — cheapest for an account reading its own mentions — so
// max is a real cost control, not a page size. The caller passes what it
// is willing to pay for.
func (c *Client) Mentions(sinceID string, max int) (*MentionPage, error) {
	id, err := c.Me()
	if err != nil {
		return nil, err
	}
	if max <= 0 {
		max = 10
	}
	if max > 100 {
		max = 100 // the endpoint's own ceiling
	}
	q := map[string]string{
		"max_results":  fmt.Sprint(max),
		"tweet.fields": "created_at,author_id",
		"expansions":   "author_id",
		"user.fields":  "username",
	}
	if sinceID != "" {
		q["since_id"] = sinceID
	}
	var out struct {
		Data     []Mention `json:"data"`
		Includes struct {
			Users []struct {
				ID       string `json:"id"`
				Username string `json:"username"`
			} `json:"users"`
		} `json:"includes"`
		Meta struct {
			NewestID string `json:"newest_id"`
		} `json:"meta"`
	}
	if err := c.get("/users/"+id+"/mentions", q, &out); err != nil {
		return nil, err
	}
	// author_id is a number nobody can read. The usernames come back in a
	// separate expansion block, so stitch them on here rather than making
	// every caller do it.
	handle := map[string]string{}
	for _, u := range out.Includes.Users {
		handle[u.ID] = "@" + u.Username
	}
	for i := range out.Data {
		out.Data[i].Author = handle[out.Data[i].AuthorID]
	}
	newest := out.Meta.NewestID
	if newest == "" && len(out.Data) > 0 {
		// X documents meta.newest_id on this endpoint, but a response
		// without it must not silently leave a watch's bookmark empty —
		// that is how you go back to paying for the same posts every run.
		// The list is newest first.
		newest = out.Data[0].ID
	}
	return &MentionPage{Mentions: out.Data, NewestID: newest}, nil
}

// get performs an authenticated GET and decodes into v.
func (c *Client) get(path string, query map[string]string, v any) error {
	req, err := http.NewRequest(http.MethodGet, c.BaseURL+path, nil)
	if err != nil {
		return err
	}
	if len(query) > 0 {
		q := req.URL.Query()
		for k, val := range query {
			q.Set(k, val)
		}
		req.URL.RawQuery = q.Encode()
	}
	req.Header.Set("Authorization", "Bearer "+c.AccessToken)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("x: GET %s: %w", path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("x: GET %s rejected (%d): %s", path, resp.StatusCode, truncate(raw))
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return fmt.Errorf("x: parse %s response: %w", path, err)
	}
	return nil
}
