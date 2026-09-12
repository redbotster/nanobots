package llm

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/redbotster/nanobots/internal/schema"
)

// OpenAI talks to the chat-completions shape — which is not one provider
// but a de facto wire format. api.openai.com speaks it, and so do
// OpenRouter, Together, Groq, Fireworks, DeepInfra, vLLM, LiteLLM and
// Ollama. Point BaseURL at any of them and this backend serves them all,
// which is why there is no separate backend per gateway.
//
// It does NOT serve 1Claw's Shroud, despite Shroud exposing
// /v1/chat/completions. Shroud requires an X-Shroud-Provider header that no
// chat-completions client sends, and authenticates with an agent id and key
// rather than a bare bearer token. That is Shroud doing its job — it has to
// know which provider a call is being billed and screened against — so it
// gets its own backend rather than being bent into this one.
type OpenAI struct {
	APIKey string
	// BaseURL includes the version segment, e.g. https://api.openai.com/v1
	// or http://localhost:11434/v1 for Ollama.
	BaseURL string
	// Model is used when a bot asks for a provider this backend doesn't
	// serve, and is the only sensible setting for a gateway whose model
	// names are its own (openrouter's "anthropic/claude-...", say).
	Model string

	HTTPClient   *http.Client
	OnSubstitute func(note string)
}

const (
	openAIDefaultBase  = "https://api.openai.com/v1"
	openAIDefaultModel = "gpt-4o-mini"
)

func NewOpenAI(apiKey, baseURL, model string) *OpenAI {
	if baseURL == "" {
		baseURL = openAIDefaultBase
	}
	if model == "" {
		model = openAIDefaultModel
	}
	return &OpenAI{APIKey: apiKey, BaseURL: baseURL, Model: model, HTTPClient: newHTTPClient()}
}

func (o *OpenAI) Describe() string {
	if base := strings.TrimRight(o.BaseURL, "/"); base != openAIDefaultBase {
		// Naming the endpoint matters: "openai" on a Settings page while
		// the traffic goes to a local Ollama is a lie about where the
		// prompts are going.
		return "openai-compatible (" + base + ")"
	}
	return "openai (direct)"
}

type openAIRequest struct {
	Model       string          `json:"model"`
	Messages    []openAIMessage `json:"messages"`
	MaxTokens   int             `json:"max_tokens,omitempty"`
	Temperature *float64        `json:"temperature,omitempty"`
}

type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIResponse struct {
	Choices []struct {
		Message openAIMessage `json:"message"`
	} `json:"choices"`
}

func (o *OpenAI) Generate(ctx context.Context, prompt string, model schema.Model) (string, error) {
	name, substituted := resolveModel("openai", o.Model, model)
	if substituted && o.OnSubstitute != nil {
		if note := substitutionNote(model, "openai", name); note != "" {
			o.OnSubstitute(note)
		}
	}
	req := openAIRequest{
		Model:     name,
		Messages:  []openAIMessage{{Role: "user", Content: prompt}},
		MaxTokens: maxTokensFor(model),
	}
	if model.Temperature != 0 {
		t := model.Temperature
		req.Temperature = &t
	}

	var out openAIResponse
	err := postJSON(ctx, o.client(), strings.TrimRight(o.BaseURL, "/")+"/chat/completions", map[string]string{
		"Authorization": "Bearer " + o.APIKey,
	}, req, &out)
	if err != nil {
		return "", err
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("llm: response had no choices")
	}
	return out.Choices[0].Message.Content, nil
}

func (o *OpenAI) client() *http.Client {
	if o.HTTPClient != nil {
		return o.HTTPClient
	}
	return newHTTPClient()
}

var _ Generator = (*OpenAI)(nil)
