package linkedin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const apiBase = "https://api.linkedin.com/v2"

// Client is a real LinkedIn API v2 client bound to one member's OAuth2
// access token.
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

func (c *Client) authed(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+c.AccessToken)
	req.Header.Set("X-Restli-Protocol-Version", "2.0.0")
}

// UserInfo resolves the connected member's own id (the "sub" claim from
// LinkedIn's OpenID Connect endpoint) into the URN posts.publish needs as
// an author. Requires the "openid profile" scopes.
func (c *Client) UserInfo() (personURN string, err error) {
	req, err := http.NewRequest(http.MethodGet, c.BaseURL+"/userinfo", nil)
	if err != nil {
		return "", err
	}
	c.authed(req)
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("linkedin: userinfo: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("linkedin: userinfo rejected (%d): %s", resp.StatusCode, truncate(raw))
	}
	var out struct {
		Sub string `json:"sub"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("linkedin: parse userinfo response: %w", err)
	}
	if out.Sub == "" {
		return "", fmt.Errorf("linkedin: userinfo response had no sub claim: %s", truncate(raw))
	}
	return "urn:li:person:" + out.Sub, nil
}

// PostShare publishes a text post as personURN (from UserInfo) and returns
// its id.
func (c *Client) PostShare(personURN, text string) (string, error) {
	body, err := json.Marshal(map[string]any{
		"author":         personURN,
		"lifecycleState": "PUBLISHED",
		"specificContent": map[string]any{
			"com.linkedin.ugc.ShareContent": map[string]any{
				"shareCommentary":    map[string]string{"text": text},
				"shareMediaCategory": "NONE",
			},
		},
		"visibility": map[string]string{
			"com.linkedin.ugc.MemberNetworkVisibility": "PUBLIC",
		},
	})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequest(http.MethodPost, c.BaseURL+"/ugcPosts", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	c.authed(req)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("linkedin: post share: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("linkedin: post share rejected (%d): %s", resp.StatusCode, truncate(raw))
	}
	// LinkedIn returns the new post's id in the X-RestLi-Id response
	// header, not (reliably) in the JSON body.
	if id := resp.Header.Get("X-RestLi-Id"); id != "" {
		return id, nil
	}
	var out struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &out); err == nil && out.ID != "" {
		return out.ID, nil
	}
	return "", fmt.Errorf("linkedin: post share response had no id (header or body): %s", truncate(raw))
}

func truncate(b []byte) string {
	const max = 400
	s := string(b)
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}

// Comment is one comment on a post.
type Comment struct {
	ID     string `json:"id"`
	Text   string `json:"text"`
	Author string `json:"author"` // the commenter's URN
	At     string `json:"created_at"`
}

// Comments returns comments on one post, newest first.
//
// This needs the Community Management API, which is not part of the default
// "Sign In with LinkedIn" product: it is a separate product you add to your
// app and LinkedIn approves per-app. Until that approval lands, this
// returns LinkedIn's own 403 and the bot stays on demo fixtures. That is
// the honest state, not a bug to work around — see bots/linkedin-comments.
//
// postURN is the full URN of the post, e.g. "urn:li:share:7123456789".
func (c *Client) Comments(postURN string, max int) ([]Comment, error) {
	if postURN == "" {
		return nil, fmt.Errorf("linkedin: comments need a post urn")
	}
	if max <= 0 {
		max = 20
	}
	endpoint := fmt.Sprintf("%s/socialActions/%s/comments?count=%d",
		c.BaseURL, url.PathEscape(postURN), max)
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	c.authed(req)
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("linkedin: comments: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("linkedin: reading comments needs the Community Management API product, "+
			"which LinkedIn approves per app — this account's app does not have it yet (403): %s", truncate(raw))
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("linkedin: comments rejected (%d): %s", resp.StatusCode, truncate(raw))
	}
	var out struct {
		Elements []struct {
			ID      string `json:"id"`
			Actor   string `json:"actor"`
			Created struct {
				Time int64 `json:"time"`
			} `json:"created"`
			Message struct {
				Text string `json:"text"`
			} `json:"message"`
		} `json:"elements"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("linkedin: parse comments: %w", err)
	}
	cs := make([]Comment, 0, len(out.Elements))
	for _, e := range out.Elements {
		at := ""
		if e.Created.Time > 0 {
			at = time.UnixMilli(e.Created.Time).UTC().Format(time.RFC3339)
		}
		cs = append(cs, Comment{ID: e.ID, Text: e.Message.Text, Author: e.Actor, At: at})
	}
	return cs, nil
}
