package memory

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Honcho is recall-capable memory backed by a Honcho server
// (github.com/plastic-labs/honcho): observations go in as messages, and a
// background deriver builds a representation you can then question in plain
// language.
//
// Every path and body shape below was read out of Honcho's own source —
// src/main.go's `/v3` prefix, src/routers/{peers,messages}.py for the
// routes, src/schemas/api.py for the fields — not inferred from prose. That
// is the same standard internal/oneclaw holds itself to, and it matters
// more here because nothing in this repo can reach a real Honcho to catch a
// wrong guess.
//
// Honcho is deliberately NOT a Store. It has no key/value semantics, and
// bolting Get/Put onto messages-and-representations would be a lie about
// what it does. Pair it with Local through Composite: `last_run_at` on
// disk, "what does this person care about" in Honcho.
type Honcho struct {
	// BaseURL is the server root, without the /v3 prefix — e.g.
	// http://localhost:8000 for a self-hosted `honcho start`.
	BaseURL string
	// Workspace scopes everything. Honcho creates one on first use.
	Workspace string
	// APIKey is a bearer token. Self-hosted Honcho runs with
	// AUTH_USE_AUTH=false by default, in which case leave this empty.
	APIKey string

	HTTPClient *http.Client
}

func NewHoncho(baseURL, workspace, apiKey string) *Honcho {
	return &Honcho{
		BaseURL:    strings.TrimRight(baseURL, "/"),
		Workspace:  workspace,
		APIKey:     apiKey,
		HTTPClient: &http.Client{Timeout: 60 * time.Second},
	}
}

func (h *Honcho) do(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		raw, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, h.BaseURL+"/v3"+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if h.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+h.APIKey)
	}
	resp, err := h.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("honcho: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("honcho: %s %s returned %d: %s", method, path, resp.StatusCode, truncate(raw))
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("honcho: parse %s response: %w", path, err)
	}
	return nil
}

// peerAndSession maps a nanobots namespace onto Honcho's model. A namespace
// is a bot's own memory scope, so it becomes the peer; the session is fixed
// per namespace because a bot's runs are one continuing relationship rather
// than separate conversations.
func (h *Honcho) peerAndSession(namespace string) (peer, session string) {
	return namespace, namespace + "-runs"
}

// messageCreate mirrors src/schemas/api.py's MessageCreate. peer_name is
// declared there with alias "peer_id", which is the name on the wire.
type messageCreate struct {
	Content string `json:"content"`
	PeerID  string `json:"peer_id"`
}

type messageBatchCreate struct {
	Messages []messageCreate `json:"messages"`
}

// Remember records one observation as a message on the namespace's session.
// Honcho's deriver builds the representation from these in the background,
// so a nil return means accepted, not yet reasoned over.
func (h *Honcho) Remember(ctx context.Context, namespace, text string) error {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	peer, session := h.peerAndSession(namespace)
	path := fmt.Sprintf("/workspaces/%s/sessions/%s/messages",
		url.PathEscape(h.Workspace), url.PathEscape(session))
	return h.do(ctx, http.MethodPost, path,
		messageBatchCreate{Messages: []messageCreate{{Content: text, PeerID: peer}}}, nil)
}

// dialecticOptions mirrors DialecticOptions. Only `query` is required;
// streaming is left off because a step needs one complete answer.
type dialecticOptions struct {
	Query  string `json:"query"`
	Stream bool   `json:"stream"`
}

// dialecticResponse mirrors DialecticResponse: a single nullable content
// field.
type dialecticResponse struct {
	Content *string `json:"content"`
}

// Recall asks Honcho's dialectic endpoint a natural-language question about
// what it has accumulated for this namespace.
func (h *Honcho) Recall(ctx context.Context, namespace, question string) (string, error) {
	if strings.TrimSpace(question) == "" {
		return "", fmt.Errorf("recall needs a question")
	}
	peer, _ := h.peerAndSession(namespace)
	path := fmt.Sprintf("/workspaces/%s/peers/%s/chat",
		url.PathEscape(h.Workspace), url.PathEscape(peer))
	var out dialecticResponse
	if err := h.do(ctx, http.MethodPost, path, dialecticOptions{Query: question}, &out); err != nil {
		return "", err
	}
	if out.Content == nil {
		// Honcho declares content as nullable; nothing known yet is a real
		// answer, not a failure.
		return "", nil
	}
	return *out.Content, nil
}

func truncate(b []byte) string {
	const max = 300
	if len(b) <= max {
		return string(b)
	}
	return string(b[:max]) + "…"
}

var _ Recaller = (*Honcho)(nil)
