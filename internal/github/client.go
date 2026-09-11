// Package github is a minimal, real GitHub REST API client for exactly the
// op this build's bots need: listing a repo's open issues. Like Slack, a
// GitHub personal access token needs no OAuth dance — it's pasted in once
// (WebUI Settings, or `nanobots connect github`) and stored as a 1Claw vault
// secret, never on local disk or inside a bot container.
package github

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

var apiBase = "https://api.github.com"

type Client struct {
	HTTPClient *http.Client
	Token      string
}

func NewClient(token string) *Client {
	return &Client{HTTPClient: &http.Client{Timeout: 15 * time.Second}, Token: token}
}

// Issue is the shape bots/github-issues-digest/fixtures/*.json also uses —
// live and demo data share one shape so a bot's prompt doesn't need to know
// which it's looking at.
type Issue struct {
	Number    int    `json:"number"`
	Title     string `json:"title"`
	User      string `json:"user"`
	HTMLURL   string `json:"html_url"`
	CreatedAt string `json:"created_at"`
}

type rawIssue struct {
	Number  int    `json:"number"`
	Title   string `json:"title"`
	HTMLURL string `json:"html_url"`
	User    struct {
		Login string `json:"login"`
	} `json:"user"`
	CreatedAt   string `json:"created_at"`
	PullRequest any    `json:"pull_request,omitempty"`
}

// IssuesList implements `issues.list`: the most recent `max` open issues on
// repo ("owner/name"). GitHub's issues endpoint also returns pull requests
// (a PR *is* an issue in their model) — filtered out here since a bot asking
// for "issues" almost never wants PR noise mixed in.
func (c *Client) IssuesList(repo string, max int) ([]Issue, error) {
	url := fmt.Sprintf("%s/repos/%s/issues?state=open&sort=created&direction=desc&per_page=%d", apiBase, repo, max)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github: issues.list: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("github: issues.list failed (%d): %s", resp.StatusCode, truncate(raw))
	}
	var items []rawIssue
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("github: parse issues.list response: %w", err)
	}
	out := make([]Issue, 0, len(items))
	for _, it := range items {
		if it.PullRequest != nil {
			continue
		}
		out = append(out, Issue{Number: it.Number, Title: it.Title, User: it.User.Login, HTMLURL: it.HTMLURL, CreatedAt: it.CreatedAt})
	}
	return out, nil
}

func truncate(b []byte) string {
	const max = 400
	s := string(b)
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}
