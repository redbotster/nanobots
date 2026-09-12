package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/redbotster/nanobots/internal/schema"
)

// capture records what a backend actually put on the wire. Every provider
// below is asserted against its own documented request shape rather than
// against whatever the code happens to send — the same standard
// internal/memory's Honcho client is held to, and for the same reason:
// nothing in CI can reach a real provider to catch a wrong guess.
type capture struct {
	path, query string
	headers     http.Header
	body        map[string]any
}

func serve(t *testing.T, respond string) (*httptest.Server, *capture) {
	t.Helper()
	got := &capture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.path, got.query, got.headers = r.URL.Path, r.URL.RawQuery, r.Header.Clone()
		_ = json.NewDecoder(r.Body).Decode(&got.body)
		_, _ = w.Write([]byte(respond))
	}))
	t.Cleanup(srv.Close)
	return srv, got
}

func TestAnthropicSendsTheMessagesAPIShape(t *testing.T) {
	srv, got := serve(t, `{"content":[{"type":"text","text":"hello"}]}`)
	a := NewAnthropic("secret-key", "")
	a.BaseURL = srv.URL

	out, err := a.Generate(context.Background(), "say hello",
		schema.Model{Provider: "anthropic", Name: "claude-sonnet-4-6", MaxTokens: 1000, Temperature: 0.2})
	if err != nil {
		t.Fatal(err)
	}
	if out != "hello" {
		t.Errorf("out = %q", out)
	}
	if got.path != "/v1/messages" {
		t.Errorf("path = %q, want /v1/messages", got.path)
	}
	// Both headers are required; the API rejects a call missing either.
	if got.headers.Get("x-api-key") != "secret-key" {
		t.Error("x-api-key not sent")
	}
	if got.headers.Get("anthropic-version") != anthropicVersion {
		t.Errorf("anthropic-version = %q", got.headers.Get("anthropic-version"))
	}
	if got.body["model"] != "claude-sonnet-4-6" {
		t.Errorf("model = %v — the bot's own choice must be honoured", got.body["model"])
	}
	if got.body["max_tokens"] != float64(1000) {
		t.Errorf("max_tokens = %v", got.body["max_tokens"])
	}
}

// A response can carry thinking and tool_use blocks alongside text. Taking
// content[0].text would return a thinking block's empty text as the answer.
func TestAnthropicJoinsOnlyTheTextBlocks(t *testing.T) {
	srv, _ := serve(t, `{"content":[{"type":"thinking","thinking":"hmm"},{"type":"text","text":"part one "},{"type":"text","text":"part two"}]}`)
	a := NewAnthropic("k", "")
	a.BaseURL = srv.URL

	out, err := a.Generate(context.Background(), "p", schema.Model{Provider: "anthropic", Name: "m"})
	if err != nil {
		t.Fatal(err)
	}
	if out != "part one part two" {
		t.Errorf("out = %q", out)
	}
}

func TestOpenAISendsTheChatCompletionsShape(t *testing.T) {
	srv, got := serve(t, `{"choices":[{"message":{"role":"assistant","content":"hi"}}]}`)
	o := NewOpenAI("sk-test", srv.URL+"/v1", "")

	out, err := o.Generate(context.Background(), "say hi",
		schema.Model{Provider: "openai", Name: "gpt-4o-mini", MaxTokens: 500})
	if err != nil {
		t.Fatal(err)
	}
	if out != "hi" {
		t.Errorf("out = %q", out)
	}
	if got.path != "/v1/chat/completions" {
		t.Errorf("path = %q", got.path)
	}
	if got.headers.Get("Authorization") != "Bearer sk-test" {
		t.Errorf("auth = %q", got.headers.Get("Authorization"))
	}
	msgs, _ := got.body["messages"].([]any)
	if len(msgs) != 1 {
		t.Fatalf("messages = %#v", got.body["messages"])
	}
	first, _ := msgs[0].(map[string]any)
	if first["role"] != "user" || first["content"] != "say hi" {
		t.Errorf("message = %#v", first)
	}
}

// The point of this backend is that it is not one provider. A custom base
// URL has to reach the log and the status page, or someone reads "openai"
// while their prompts go to a local Ollama.
func TestOpenAIDescribesACustomEndpointRatherThanClaimingToBeOpenAI(t *testing.T) {
	if got := NewOpenAI("k", "http://localhost:11434/v1", "llama3.1").Describe(); !strings.Contains(got, "localhost:11434") {
		t.Errorf("Describe() = %q, want the real endpoint named", got)
	}
	if got := NewOpenAI("k", "", "").Describe(); got != "openai (direct)" {
		t.Errorf("Describe() = %q", got)
	}
}

func TestGeminiSendsTheGenerateContentShape(t *testing.T) {
	srv, got := serve(t, `{"candidates":[{"content":{"parts":[{"text":"yes"}]}}]}`)
	g := NewGemini("AIza-secret", "")
	g.BaseURL = srv.URL + "/v1beta"

	out, err := g.Generate(context.Background(), "is it?",
		schema.Model{Provider: "gemini", Name: "gemini-3.5-flash-lite", MaxTokens: 800, Temperature: 0.4})
	if err != nil {
		t.Fatal(err)
	}
	if out != "yes" {
		t.Errorf("out = %q", out)
	}
	if got.path != "/v1beta/models/gemini-3.5-flash-lite:generateContent" {
		t.Errorf("path = %q", got.path)
	}
	// The key is authentication here, and it goes in the query string.
	if !strings.Contains(got.query, "key=AIza-secret") {
		t.Errorf("query = %q, want the key", got.query)
	}
	cfg, _ := got.body["generationConfig"].(map[string]any)
	if cfg["maxOutputTokens"] != float64(800) {
		t.Errorf("generationConfig = %#v — Gemini spells this maxOutputTokens, not max_tokens", cfg)
	}
	if cfg["temperature"] != 0.4 {
		t.Errorf("temperature = %v", cfg["temperature"])
	}
}

// Gemini authenticates in the URL, and postJSON puts the URL it called into
// every error. Without redaction a rate-limit error would write the API key
// into the run log, the run snapshot on disk, and the WebUI.
func TestGeminiNeverLeaksItsKeyIntoAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"quota exceeded"}}`))
	}))
	defer srv.Close()

	g := NewGemini("AIzaSyTOPSECRET", "")
	g.BaseURL = srv.URL + "/v1beta"

	_, err := g.Generate(context.Background(), "p", schema.Model{Provider: "gemini", Name: "m"})
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "AIzaSyTOPSECRET") {
		t.Errorf("the API key is in the error text: %v", err)
	}
	// Redacting must not cost the reader the actual reason.
	if !strings.Contains(err.Error(), "quota exceeded") {
		t.Errorf("the provider's own message was lost: %v", err)
	}
}

// A bot declares the model it wants. When this deployment can serve that
// provider, the bot gets exactly what it asked for; when it can't, sending
// the name anyway is a guaranteed 404, so the backend substitutes — and
// must say so, because a quietly different model is a quietly different
// product.
func TestAModelTheBackendCannotServeIsSubstitutedOutLoud(t *testing.T) {
	srv, got := serve(t, `{"candidates":[{"content":{"parts":[{"text":"ok"}]}}]}`)
	var notes []string
	g := NewGemini("k", "gemini-3.5-flash-lite")
	g.BaseURL = srv.URL + "/v1beta"
	g.OnSubstitute = func(n string) { notes = append(notes, n) }

	// What every bot in this catalog actually declares.
	if _, err := g.Generate(context.Background(), "p",
		schema.Model{Provider: "anthropic", Name: "claude-sonnet-4-6"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.path, "gemini-3.5-flash-lite") {
		t.Errorf("path = %q, want the model this backend can actually serve", got.path)
	}
	if len(notes) != 1 {
		t.Fatalf("notes = %v, want one substitution note", notes)
	}
	for _, want := range []string{"claude-sonnet-4-6", "gemini", "gemini-3.5-flash-lite"} {
		if !strings.Contains(notes[0], want) {
			t.Errorf("note %q does not mention %q", notes[0], want)
		}
	}

	// And when they do match, no substitution and no note.
	notes = nil
	if _, err := g.Generate(context.Background(), "p",
		schema.Model{Provider: "gemini", Name: "gemini-3.1-flash-lite"}); err != nil {
		t.Fatal(err)
	}
	if len(notes) != 0 {
		t.Errorf("substituted a model the backend can serve: %v", notes)
	}
	if !strings.Contains(got.path, "gemini-3.1-flash-lite") {
		t.Errorf("path = %q, want the bot's own model", got.path)
	}
}

// Zero is a real temperature meaning "be deterministic". A bot that simply
// omits the field must not be silently pinned to it — that would change how
// every generative bot in the catalog behaves.
func TestTemperatureIsOnlySentWhenTheBotSetOne(t *testing.T) {
	for _, tc := range []struct {
		name string
		gen  func(*httptest.Server) Generator
		read func(*capture) (any, bool)
	}{
		{"anthropic", func(s *httptest.Server) Generator {
			a := NewAnthropic("k", "")
			a.BaseURL = s.URL
			return a
		}, func(c *capture) (any, bool) { v, ok := c.body["temperature"]; return v, ok }},
		{"openai", func(s *httptest.Server) Generator {
			return NewOpenAI("k", s.URL+"/v1", "")
		}, func(c *capture) (any, bool) { v, ok := c.body["temperature"]; return v, ok }},
		{"gemini", func(s *httptest.Server) Generator {
			g := NewGemini("k", "")
			g.BaseURL = s.URL + "/v1beta"
			return g
		}, func(c *capture) (any, bool) {
			cfg, _ := c.body["generationConfig"].(map[string]any)
			v, ok := cfg["temperature"]
			return v, ok
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, got := serve(t, `{"content":[{"type":"text","text":"x"}],"choices":[{"message":{"content":"x"}}],"candidates":[{"content":{"parts":[{"text":"x"}]}}]}`)
			if _, err := tc.gen(srv).Generate(context.Background(), "p", schema.Model{Name: "m"}); err != nil {
				t.Fatal(err)
			}
			if v, ok := tc.read(got); ok {
				t.Errorf("sent temperature=%v for a bot that declared none", v)
			}
		})
	}
}

// Anthropic rejects a request with no max_tokens outright, so a bot that
// declares none still needs a number.
func TestMaxTokensAlwaysHasAValue(t *testing.T) {
	srv, got := serve(t, `{"content":[{"type":"text","text":"x"}]}`)
	a := NewAnthropic("k", "")
	a.BaseURL = srv.URL

	if _, err := a.Generate(context.Background(), "p", schema.Model{Provider: "anthropic", Name: "m"}); err != nil {
		t.Fatal(err)
	}
	if got.body["max_tokens"] != float64(defaultMaxTokens) {
		t.Errorf("max_tokens = %v, want the default", got.body["max_tokens"])
	}
}

// The provider's own explanation is the difference between "429, go away"
// and "429, your org exceeded its quota" — one of which tells you what to
// do next.
func TestAProviderErrorKeepsItsOwnMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"type":"invalid_request_error","message":"max_tokens: must be >= 1"}}`))
	}))
	defer srv.Close()

	a := NewAnthropic("k", "")
	a.BaseURL = srv.URL
	_, err := a.Generate(context.Background(), "p", schema.Model{Provider: "anthropic", Name: "m"})
	if err == nil || !strings.Contains(err.Error(), "max_tokens: must be >= 1") {
		t.Errorf("err = %v, want the provider's own message", err)
	}
}

// DeferredShroud stands in until a bot's agent exists. It must behave as
// its fallback rather than failing, so anything generating outside a bot
// run still works.
func TestDeferredShroudFallsBackUntilAnAgentExists(t *testing.T) {
	srv, _ := serve(t, `{"candidates":[{"content":{"parts":[{"text":"from the fallback"}]}}]}`)
	g := NewGemini("k", "")
	g.BaseURL = srv.URL + "/v1beta"

	d := &DeferredShroud{Fallback: g}
	if !IsDeferredShroud(d) {
		t.Error("IsDeferredShroud did not recognise it")
	}
	out, err := d.Generate(context.Background(), "p", schema.Model{Provider: "gemini", Name: "m"})
	if err != nil {
		t.Fatal(err)
	}
	if out != "from the fallback" {
		t.Errorf("out = %q", out)
	}

	// With nothing to fall back to it says what's missing, rather than
	// returning an empty answer a bot would treat as a real one.
	if _, err := (&DeferredShroud{}).Generate(context.Background(), "p", schema.Model{}); err == nil {
		t.Error("expected ErrNoGenerator")
	}
}

// The very first real Gemini call this package made returned 503 "model is
// overloaded" and took down a swarm that had already done real work; the
// identical call succeeded seconds later. Providers 429 and 503 as a matter
// of course, so one blip must not be fatal.
func TestATransientProviderFailureIsRetried(t *testing.T) {
	var attempts int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":{"message":"model is overloaded"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"eventually"}]}`))
	}))
	defer srv.Close()

	a := NewAnthropic("k", "")
	a.BaseURL = srv.URL
	// Real waits would make this test sleep for three seconds.
	a.HTTPClient = srv.Client()

	out, err := a.Generate(context.Background(), "p", schema.Model{Provider: "anthropic", Name: "m"})
	if err != nil {
		t.Fatalf("gave up on a transient failure: %v", err)
	}
	if out != "eventually" {
		t.Errorf("out = %q", out)
	}
	if attempts != 3 {
		t.Errorf("attempts = %d, want 3", attempts)
	}
}

// A bad request is the provider saying "not ever". Retrying it just burns
// the run's clock and, on a metered endpoint, its budget.
func TestAPermanentFailureIsNotRetried(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusNotFound} {
		var attempts int
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			attempts++
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"error":{"message":"nope"}}`))
		}))
		a := NewAnthropic("k", "")
		a.BaseURL = srv.URL
		_, err := a.Generate(context.Background(), "p", schema.Model{Provider: "anthropic", Name: "m"})
		srv.Close()
		if err == nil {
			t.Fatalf("%d: expected an error", status)
		}
		if attempts != 1 {
			t.Errorf("%d: attempts = %d, want 1 — a permanent failure was retried", status, attempts)
		}
	}
}

// When the provider says how long to wait, believe it — but not past the
// point where holding a container open costs more than failing. A quota
// window measured in minutes is a real failure, not a blip to sit through.
func TestRetryAfterIsHonouredButNotIndefinitely(t *testing.T) {
	if got := retryAfter(http.Header{"Retry-After": []string{"5"}}, 0); got != 5*time.Second {
		t.Errorf("Retry-After: 5 -> %v, want 5s", got)
	}
	if got := retryAfter(http.Header{"Retry-After": []string{"3600"}}, 0); got != 0 {
		t.Errorf("an hour-long Retry-After -> %v, want 0 (give up now)", got)
	}
	// No header: plain exponential backoff.
	if got := retryAfter(http.Header{}, 0); got != time.Second {
		t.Errorf("attempt 0 -> %v, want 1s", got)
	}
	if got := retryAfter(http.Header{}, 1); got != 2*time.Second {
		t.Errorf("attempt 1 -> %v, want 2s", got)
	}
	// Nonsense from the provider falls back to backoff rather than panicking.
	if got := retryAfter(http.Header{"Retry-After": []string{"Wed, 21 Oct 2026 07:28:00 GMT"}}, 0); got != time.Second {
		t.Errorf("http-date Retry-After -> %v, want the backoff default", got)
	}
}

// A retry loop that ignores cancellation would keep a killed run's
// container alive through its backoff.
func TestRetriesStopWhenTheContextIsCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	a := NewAnthropic("k", "")
	a.BaseURL = srv.URL
	if _, err := a.Generate(ctx, "p", schema.Model{Provider: "anthropic", Name: "m"}); err == nil {
		t.Error("expected a cancellation error")
	}
}
