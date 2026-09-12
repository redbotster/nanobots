package llm

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/redbotster/nanobots/internal/schema"
)

// Gemini talks to generativelanguage.googleapis.com directly.
//
// Worth knowing before relying on it: the free tier is small and counted
// per minute per model — five generate_content calls a minute on Flash at
// the time of writing. A swarm of four bots each making one call is
// already at the edge. That is not a reason to avoid it (it is the easiest
// key in the world to get, and it is what this repo's local Honcho uses),
// but it is a reason a rate-limit error has to read clearly, which is why
// postJSON keeps Google's own message.
type Gemini struct {
	APIKey string
	// BaseURL defaults to the v1beta endpoint, which is where the current
	// models live.
	BaseURL string
	// Model is used when a bot asks for a provider this backend doesn't
	// serve — which, for this catalog, is nearly every bot, since they
	// declare anthropic.
	Model string

	HTTPClient   *http.Client
	OnSubstitute func(note string)
}

const (
	geminiDefaultBase = "https://generativelanguage.googleapis.com/v1beta"
	// A lite model by default: the catalog's prompts are short and
	// structured, and the free tier gives lite models more headroom.
	// Pinned rather than a -latest alias because Google retires models for
	// new keys (gemini-2.5-flash already 404s for keys created after its
	// retirement), and a default that changes under you between runs is
	// worse than one you update deliberately.
	geminiDefaultModel = "gemini-3.5-flash-lite"
)

func NewGemini(apiKey, model string) *Gemini {
	if model == "" {
		model = geminiDefaultModel
	}
	return &Gemini{APIKey: apiKey, BaseURL: geminiDefaultBase, Model: model, HTTPClient: newHTTPClient()}
}

func (g *Gemini) Describe() string { return "gemini (direct)" }

type geminiRequest struct {
	Contents         []geminiContent `json:"contents"`
	GenerationConfig geminiGenConfig `json:"generationConfig"`
}

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text string `json:"text"`
}

type geminiGenConfig struct {
	MaxOutputTokens int      `json:"maxOutputTokens,omitempty"`
	Temperature     *float64 `json:"temperature,omitempty"`
}

type geminiResponse struct {
	Candidates []struct {
		Content struct {
			Parts []geminiPart `json:"parts"`
		} `json:"content"`
		FinishReason string `json:"finishReason"`
	} `json:"candidates"`
}

func (g *Gemini) Generate(ctx context.Context, prompt string, model schema.Model) (string, error) {
	name, substituted := resolveModel("gemini", g.Model, model)
	if substituted && g.OnSubstitute != nil {
		if note := substitutionNote(model, "gemini", name); note != "" {
			g.OnSubstitute(note)
		}
	}
	req := geminiRequest{
		Contents:         []geminiContent{{Role: "user", Parts: []geminiPart{{Text: prompt}}}},
		GenerationConfig: geminiGenConfig{MaxOutputTokens: maxTokensFor(model)},
	}
	if model.Temperature != 0 {
		t := model.Temperature
		req.GenerationConfig.Temperature = &t
	}

	// The key goes in the query string, which is how this API is
	// authenticated. Everything that logs a URL in this package logs the
	// path only, never the raw URL, for that reason.
	endpoint := fmt.Sprintf("%s/models/%s:generateContent?key=%s",
		strings.TrimRight(g.BaseURL, "/"), url.PathEscape(name), url.QueryEscape(g.APIKey))

	var out geminiResponse
	if err := postJSON(ctx, g.client(), endpoint, nil, req, &out); err != nil {
		return "", redactKey(err, g.APIKey)
	}
	if len(out.Candidates) == 0 {
		return "", fmt.Errorf("llm: gemini returned no candidates (the prompt may have been blocked)")
	}
	var b strings.Builder
	for _, p := range out.Candidates[0].Content.Parts {
		b.WriteString(p.Text)
	}
	if b.Len() == 0 {
		return "", fmt.Errorf("llm: gemini returned an empty response (finishReason=%s)",
			out.Candidates[0].FinishReason)
	}
	return b.String(), nil
}

// redactKey keeps the API key out of an error string. postJSON includes the
// URL it called, and for this provider the URL contains the credential —
// which would otherwise land in a run log, a run snapshot on disk, and the
// WebUI.
func redactKey(err error, key string) error {
	if err == nil || key == "" {
		return err
	}
	msg := err.Error()
	for _, form := range []string{key, url.QueryEscape(key)} {
		msg = strings.ReplaceAll(msg, form, "REDACTED")
	}
	return fmt.Errorf("%s", msg)
}

func (g *Gemini) client() *http.Client {
	if g.HTTPClient != nil {
		return g.HTTPClient
	}
	return newHTTPClient()
}

var _ Generator = (*Gemini)(nil)
