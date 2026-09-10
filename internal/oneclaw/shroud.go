package oneclaw

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const DefaultShroudURL = "https://shroud.1claw.co"

// ShroudClient talks to 1Claw's Shroud LLM proxy directly — a different host
// than the Human API, authenticated with an agent's own id:api_key pair
// rather than the human bearer token (see docs/oneclaw-bridge.md).
type ShroudClient struct {
	BaseURL    string
	HTTPClient *http.Client
	AgentID    string
	AgentKey   string // ocv_...
}

func NewShroudClient(agentID, agentKey string) *ShroudClient {
	return &ShroudClient{
		BaseURL:    DefaultShroudURL,
		HTTPClient: &http.Client{Timeout: 60 * time.Second},
		AgentID:    agentID,
		AgentKey:   agentKey,
	}
}

type shroudMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type shroudChatRequest struct {
	Model     string          `json:"model"`
	Messages  []shroudMessage `json:"messages"`
	MaxTokens int             `json:"max_tokens,omitempty"`
}

type shroudChatResponse struct {
	Choices []struct {
		Message shroudMessage `json:"message"`
	} `json:"choices"`
}

// Chat sends one user-role prompt through Shroud and returns the assistant's
// text response. provider/model come from the bot's declared spec.model.
func (s *ShroudClient) Chat(provider, model, prompt string, maxTokens int) (string, error) {
	body, err := json.Marshal(shroudChatRequest{
		Model:     model,
		Messages:  []shroudMessage{{Role: "user", Content: prompt}},
		MaxTokens: maxTokens,
	})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequest(http.MethodPost, s.BaseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Shroud-Agent-Key", s.AgentID+":"+s.AgentKey)
	req.Header.Set("X-Shroud-Provider", provider)

	resp, err := s.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("shroud: request failed: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("shroud: chat failed (%d): %s", resp.StatusCode, truncate(raw))
	}
	var cr shroudChatResponse
	if err := json.Unmarshal(raw, &cr); err != nil {
		return "", fmt.Errorf("shroud: parse response: %w", err)
	}
	if len(cr.Choices) == 0 {
		return "", fmt.Errorf("shroud: response had no choices (body: %s)", truncate(raw))
	}
	return cr.Choices[0].Message.Content, nil
}
