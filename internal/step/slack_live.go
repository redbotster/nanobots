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
	TokenKey string
}

func (cfg SlackConfig) tokenConfig() VaultTokenConfig {
	key := cfg.TokenKey
	if key == "" {
		key = "slack/bot_token"
	}
	return VaultTokenConfig{Key: key}
}

type slackAPI interface {
	PostMessage(channel, text string) (string, error)
}

func (l *LiveDeps) slackClient() (slackAPI, error) {
	cfg := l.Services.Slack.tokenConfig()
	if l.slackTokenCache == nil {
		l.slackTokenCache = &vaultToken{store: l.Secrets, cfg: cfg}
	}
	token, err := l.slackTokenCache.Get()
	if err != nil {
		return nil, wrapTokenErr("slack", "connect a bot token from Settings", err)
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

// dispatchSlack serves a bot's `service.call` against Slack.
//
// One op, because one is what the catalog uses: lead-router posts an alert
// to a channel. Adding more is a line each, and inventing them before a bot
// wants one is how a client grows methods nothing calls.
func dispatchSlack(c slackAPI, op string, params map[string]any) (any, error) {
	switch op {
	case "messages.post":
		channel, _ := params["channel"].(string)
		text, _ := params["text"].(string)
		if channel == "" {
			return nil, fmt.Errorf("slack: messages.post needs a channel")
		}
		ts, err := c.PostMessage(channel, text)
		if err != nil {
			return nil, err
		}
		// "ts" is Slack's own name for a message id, and it is what a
		// downstream bot would need to thread a reply.
		return map[string]any{"ts": ts, "channel": channel}, nil
	default:
		return nil, fmt.Errorf("slack: unsupported op %q", op)
	}
}
