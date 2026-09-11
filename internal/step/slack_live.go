package step

import (
	"fmt"
	"strings"

	"github.com/redbotster/nanobots/internal/slack"
)

// SlackConfig configures real delivery for `notify` steps whose channel
// targets Slack (see slackChannelFrom). TokenKey defaults to
// "slack/bot_token" in the same 1Claw vault Google/GitHub use.
type SlackConfig struct {
	VaultID  string
	TokenKey string
}

func (cfg SlackConfig) tokenConfig() VaultTokenConfig {
	key := cfg.TokenKey
	if key == "" {
		key = "slack/bot_token"
	}
	return VaultTokenConfig{VaultID: cfg.VaultID, Key: key}
}

type slackAPI interface {
	PostMessage(channel, text string) (string, error)
}

func (l *LiveDeps) slackClient() (slackAPI, error) {
	cfg := l.Slack.tokenConfig()
	if !cfg.configured() {
		return nil, fmt.Errorf("slack: not configured — connect a bot token from Settings")
	}
	if l.slackTokenCache == nil {
		l.slackTokenCache = &vaultToken{oc: l.OneClaw, cfg: cfg}
	}
	token, err := l.slackTokenCache.Get()
	if err != nil {
		return nil, err
	}
	return slack.NewClient(token), nil
}

// slackChannelFrom recognizes a `notify` step's channel input targeting
// Slack — "slack:#general" or "slack:C0123..." — returning the bare channel
// Slack's API expects. A channel with no recognized prefix (e.g. "email:...",
// "sms:...") isn't Slack's to handle; Notify falls back to DemoDeps for
// those, a disclosed gap rather than a silent success.
func slackChannelFrom(channel string) (target string, ok bool) {
	return strings.CutPrefix(channel, "slack:")
}
