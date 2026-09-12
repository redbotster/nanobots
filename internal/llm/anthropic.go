package llm

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/redbotster/nanobots/internal/schema"
)

// Anthropic talks to api.anthropic.com's Messages API directly.
//
// No 1Claw in the path, which means no budget ceiling, no PII redaction and
// no injection screening — the three things Shroud adds. That trade is the
// user's to make and it is stated in docs/llm.md and in Settings, not
// hidden here.
type Anthropic struct {
	APIKey string
	// BaseURL defaults to https://api.anthropic.com. Overridable for a
	// gateway that speaks the same shape, and for tests.
	BaseURL string
	// Model is used when a bot asks for a provider this backend doesn't
	// serve. See resolveModel.
	Model string

	HTTPClient *http.Client
	// OnSubstitute, when set, is told each time a bot's declared model was
	// swapped for this backend's own. The runner points it at the run log
	// so a downgrade is never silent.
	OnSubstitute func(note string)
}

const (
	anthropicDefaultBase  = "https://api.anthropic.com"
	anthropicDefaultModel = "claude-sonnet-4-6"
	// anthropicVersion is required on every request; the API rejects a
	// call without it rather than assuming a default.
	anthropicVersion = "2023-06-01"
)

func NewAnthropic(apiKey, model string) *Anthropic {
	if model == "" {
		model = anthropicDefaultModel
	}
	return &Anthropic{APIKey: apiKey, BaseURL: anthropicDefaultBase, Model: model, HTTPClient: newHTTPClient()}
}

func (a *Anthropic) Describe() string { return "anthropic (direct)" }

type anthropicRequest struct {
	Model       string             `json:"model"`
	MaxTokens   int                `json:"max_tokens"`
	Temperature *float64           `json:"temperature,omitempty"`
	Messages    []anthropicMessage `json:"messages"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

func (a *Anthropic) Generate(ctx context.Context, prompt string, model schema.Model) (string, error) {
	name, substituted := resolveModel("anthropic", a.Model, model)
	if substituted && a.OnSubstitute != nil {
		if note := substitutionNote(model, "anthropic", name); note != "" {
			a.OnSubstitute(note)
		}
	}
	req := anthropicRequest{
		Model:     name,
		MaxTokens: maxTokensFor(model),
		Messages:  []anthropicMessage{{Role: "user", Content: prompt}},
	}
	// Only sent when the bot set one: zero is a legitimate temperature
	// meaning "be deterministic", and sending it for every bot that simply
	// omitted the field would change how the whole catalog behaves.
	if model.Temperature != 0 {
		t := model.Temperature
		req.Temperature = &t
	}

	var out anthropicResponse
	err := postJSON(ctx, a.client(), strings.TrimRight(a.BaseURL, "/")+"/v1/messages", map[string]string{
		"x-api-key":         a.APIKey,
		"anthropic-version": anthropicVersion,
	}, req, &out)
	if err != nil {
		return "", err
	}
	// A response can carry several blocks (text, thinking, tool_use); join
	// the text ones rather than assuming the first block is the answer.
	var b strings.Builder
	for _, c := range out.Content {
		if c.Type == "text" {
			b.WriteString(c.Text)
		}
	}
	if b.Len() == 0 {
		return "", fmt.Errorf("llm: anthropic returned no text content")
	}
	return b.String(), nil
}

func (a *Anthropic) client() *http.Client {
	if a.HTTPClient != nil {
		return a.HTTPClient
	}
	return newHTTPClient()
}

var _ Generator = (*Anthropic)(nil)
