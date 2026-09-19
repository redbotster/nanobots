package step

import (
	"fmt"

	"github.com/redbotster/nanobots/internal/github"
)

// GitHubConfig configures direct GitHub access for a service with
// provider: github and a non-demo connection. TokenKey defaults to
// "github/token" in the same 1Claw vault Google/Slack use.
type GitHubConfig struct {
	TokenKey string
}

func (cfg GitHubConfig) tokenConfig() VaultTokenConfig {
	key := cfg.TokenKey
	if key == "" {
		key = "github/token"
	}
	return VaultTokenConfig{Key: key}
}

// githubAPI is the subset of *github.Client's methods dispatchGitHub calls —
// narrow and fake-able, matching this package's googleAPI/BlobStore pattern.
type githubAPI interface {
	IssuesList(repo string, max int) ([]github.Issue, error)
}

func (l *LiveDeps) githubClient() (githubAPI, error) {
	cfg := l.Services.GitHub.tokenConfig()
	if l.githubTokenCache == nil {
		l.githubTokenCache = &vaultToken{store: l.Secrets, cfg: cfg}
	}
	token, err := l.githubTokenCache.Get()
	if err != nil {
		return nil, wrapTokenErr("github", "connect a personal access token from Settings", err)
	}
	return github.NewClient(token), nil
}

// dispatchGitHub maps a service.call op onto the real client, shaping the
// result as generic JSON (toJSONAny, from google_live.go) so lookupPath can
// walk it exactly like a fixture's raw JSON.
func dispatchGitHub(c githubAPI, op string, params map[string]any) (any, error) {
	switch op {
	case "issues.list":
		repo, _ := params["repo"].(string)
		issues, err := c.IssuesList(repo, paramInt(params["max"], 20))
		if err != nil {
			return nil, err
		}
		return toJSONAny(issues)
	default:
		return nil, fmt.Errorf("github: unsupported op %q", op)
	}
}
