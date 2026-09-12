package memory

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalRoundTrips(t *testing.T) {
	l, err := NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	if _, found, err := l.Get(ctx, "competitor-watch", "last_summary"); err != nil || found {
		t.Fatalf("empty store: found=%v err=%v", found, err)
	}
	if err := l.Put(ctx, "competitor-watch", "last_summary", "they shipped pricing"); err != nil {
		t.Fatal(err)
	}
	got, found, err := l.Get(ctx, "competitor-watch", "last_summary")
	if err != nil || !found || got != "they shipped pricing" {
		t.Errorf("got %q found=%v err=%v", got, found, err)
	}

	// A second key in the same namespace must not clobber the first — the
	// whole namespace is one file.
	if err := l.Put(ctx, "competitor-watch", "last_run_at", "2026-09-12"); err != nil {
		t.Fatal(err)
	}
	if got, _, _ := l.Get(ctx, "competitor-watch", "last_summary"); got != "they shipped pricing" {
		t.Errorf("second Put clobbered the first key: %q", got)
	}
	// Namespaces are separate.
	if _, found, _ := l.Get(ctx, "other-bot", "last_summary"); found {
		t.Error("namespaces are leaking into each other")
	}
}

// A namespace is a bot-supplied string, so it must not be able to write
// outside the memory directory.
func TestLocalNamespaceCannotEscapeItsDirectory(t *testing.T) {
	dir := t.TempDir()
	l, err := NewLocal(filepath.Join(dir, "mem"))
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Put(context.Background(), "../../escaped", "k", "v"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "escaped.json")); err == nil {
		t.Error("a namespace wrote outside the memory directory")
	}
	entries, _ := os.ReadDir(filepath.Join(dir, "mem"))
	if len(entries) != 1 {
		t.Fatalf("expected exactly one file inside the memory dir, got %d", len(entries))
	}
	// It still round-trips — hashing the unsafe name must not break it.
	if got, found, _ := l.Get(context.Background(), "../../escaped", "k"); !found || got != "v" {
		t.Errorf("hashed namespace did not round-trip: %q found=%v", got, found)
	}
}

func TestLocalRejectsEmptyNamespaceOrKey(t *testing.T) {
	l, _ := NewLocal(t.TempDir())
	if err := l.Put(context.Background(), "", "k", "v"); err == nil {
		t.Error("empty namespace should be rejected")
	}
	if err := l.Put(context.Background(), "ns", "", "v"); err == nil {
		t.Error("empty key should be rejected")
	}
}

// A key/value backend cannot answer questions, and must say so rather than
// returning nothing — a bot behaving as if it remembered nothing is a
// silent wrong answer.
func TestRecallAgainstAKeyValueBackendSaysSo(t *testing.T) {
	l, _ := NewLocal(t.TempDir())
	_, err := Recall(context.Background(), l, "ns", "what do they care about?")
	if !errors.Is(err, ErrNoRecall) {
		t.Errorf("err = %v, want ErrNoRecall", err)
	}
	// Remember, by contrast, is a no-op: noting something down shouldn't
	// fail a run just because this deployment is key/value only.
	if err := Remember(context.Background(), l, "ns", "an observation"); err != nil {
		t.Errorf("Remember on a key/value backend should be a no-op, got %v", err)
	}
}

func TestCompositeSplitsKeyValueFromRecall(t *testing.T) {
	l, _ := NewLocal(t.TempDir())
	rich := &fakeRecaller{}
	c := &Composite{KV: l, Rich: rich}
	ctx := context.Background()

	if err := c.Put(ctx, "ns", "k", "v"); err != nil {
		t.Fatal(err)
	}
	if got, _, _ := l.Get(ctx, "ns", "k"); got != "v" {
		t.Error("key/value did not reach the KV backend")
	}
	if err := c.Remember(ctx, "ns", "they prefer mornings"); err != nil {
		t.Fatal(err)
	}
	if len(rich.remembered) != 1 {
		t.Error("Remember did not reach the recall backend")
	}
	if got, _ := c.Recall(ctx, "ns", "when?"); got != "answered: when?" {
		t.Errorf("Recall = %q", got)
	}

	// Without a rich half it is honestly key/value only.
	plain := &Composite{KV: l}
	if _, err := plain.Recall(ctx, "ns", "?"); !errors.Is(err, ErrNoRecall) {
		t.Errorf("err = %v, want ErrNoRecall", err)
	}
}

type fakeRecaller struct{ remembered []string }

func (f *fakeRecaller) Remember(_ context.Context, _, text string) error {
	f.remembered = append(f.remembered, text)
	return nil
}
func (f *fakeRecaller) Recall(_ context.Context, _, q string) (string, error) {
	return "answered: " + q, nil
}

// Honcho's paths and body shapes were read out of its own source rather
// than guessed, and nothing in this repo can reach a real Honcho to catch a
// mistake — so the fake asserts them exactly.
func TestHonchoUsesTheRealRoutesAndShapes(t *testing.T) {
	var gotMessagePath, gotChatPath string
	var gotMessageBody, gotChatBody map[string]any
	var gotAuth string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		switch {
		case strings.HasSuffix(r.URL.Path, "/messages"):
			gotMessagePath, gotMessageBody = r.URL.Path, body
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`[]`))
		case strings.HasSuffix(r.URL.Path, "/chat"):
			gotChatPath, gotChatBody = r.URL.Path, body
			_, _ = w.Write([]byte(`{"content":"They care about billing issues most."}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	h := NewHoncho(srv.URL, "my-workspace", "tok")
	ctx := context.Background()

	if err := h.Remember(ctx, "support-triage", "the user always escalates refunds"); err != nil {
		t.Fatal(err)
	}
	// src/routers/messages.py: prefix
	// /workspaces/{workspace_id}/sessions/{session_id}/messages, under /v3.
	if want := "/v3/workspaces/my-workspace/sessions/support-triage-runs/messages"; gotMessagePath != want {
		t.Errorf("message path = %q, want %q", gotMessagePath, want)
	}
	// src/schemas/api.py: MessageBatCreate{messages: [MessageCreate]},
	// where peer_name is aliased "peer_id" on the wire.
	msgs, _ := gotMessageBody["messages"].([]any)
	if len(msgs) != 1 {
		t.Fatalf("body = %#v, want one message", gotMessageBody)
	}
	first, _ := msgs[0].(map[string]any)
	if first["content"] != "the user always escalates refunds" || first["peer_id"] != "support-triage" {
		t.Errorf("message = %#v, want content + peer_id", first)
	}

	answer, err := h.Recall(ctx, "support-triage", "what do they escalate?")
	if err != nil {
		t.Fatal(err)
	}
	if answer != "They care about billing issues most." {
		t.Errorf("answer = %q", answer)
	}
	// src/routers/peers.py: POST /{peer_id}/chat under
	// /workspaces/{workspace_id}/peers, prefixed /v3.
	if want := "/v3/workspaces/my-workspace/peers/support-triage/chat"; gotChatPath != want {
		t.Errorf("chat path = %q, want %q", gotChatPath, want)
	}
	// DialecticOptions: query is the only required field.
	if gotChatBody["query"] != "what do they escalate?" {
		t.Errorf("chat body = %#v, want a query field", gotChatBody)
	}
	if gotAuth != "Bearer tok" {
		t.Errorf("auth = %q", gotAuth)
	}
}

// DialecticResponse.content is nullable. Nothing known yet is a real
// answer, not an error.
func TestHonchoTreatsNullContentAsNothingKnown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"content":null}`))
	}))
	defer srv.Close()

	got, err := NewHoncho(srv.URL, "ws", "").Recall(context.Background(), "ns", "?")
	if err != nil {
		t.Fatalf("null content should not be an error: %v", err)
	}
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

// Self-hosted Honcho runs with AUTH_USE_AUTH=false by default, so an empty
// key must mean "send no header", not "send an empty bearer".
func TestHonchoSendsNoAuthHeaderWhenUnauthenticated(t *testing.T) {
	var sawAuth bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, sawAuth = r.Header["Authorization"]
		_, _ = w.Write([]byte(`{"content":"ok"}`))
	}))
	defer srv.Close()

	if _, err := NewHoncho(srv.URL, "ws", "").Recall(context.Background(), "ns", "?"); err != nil {
		t.Fatal(err)
	}
	if sawAuth {
		t.Error("sent an Authorization header with no API key configured")
	}
}

func TestHonchoReportsAServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"detail":"deriver is down"}`))
	}))
	defer srv.Close()

	_, err := NewHoncho(srv.URL, "ws", "").Recall(context.Background(), "ns", "?")
	if err == nil || !strings.Contains(err.Error(), "deriver is down") {
		t.Errorf("err = %v, want the server's own message", err)
	}
}
