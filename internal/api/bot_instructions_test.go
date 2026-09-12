package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/redbotster/nanobots/internal/schema"
)

// Run the editor against every real bot that has an instructions port. A
// hand-written surgical edit has to survive the actual files it will meet,
// not a synthetic sample — the same discipline setServiceConnectionInYAML's
// own test applies.
func TestSetInstructionsDefaultAgainstRealBotFiles(t *testing.T) {
	botsDir := filepath.Join(repoRoot(t), "bots")
	entries, err := os.ReadDir(botsDir)
	if err != nil {
		t.Fatal(err)
	}

	checked := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		path := filepath.Join(botsDir, e.Name(), "nanobot.yaml")
		nb, err := schema.LoadNanobot(path)
		if err != nil || !hasInstructionsPort(nb) {
			continue
		}
		checked++

		t.Run(e.Name(), func(t *testing.T) {
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			// Something with the characters most likely to break a
			// hand-rolled quoter.
			const want = `Say "hello" \ not goodbye — and use 'quotes' freely.`
			edited, err := setInstructionsDefaultInYAML(raw, want)
			if err != nil {
				t.Fatalf("edit: %v", err)
			}

			tmp := filepath.Join(t.TempDir(), "nanobot.yaml")
			if err := os.WriteFile(tmp, edited, 0o644); err != nil {
				t.Fatal(err)
			}
			reloaded, err := schema.LoadNanobot(tmp)
			if err != nil {
				t.Fatalf("edited bot no longer loads: %v", err)
			}
			var got string
			for _, p := range reloaded.Spec.Ports.Inputs {
				if p.Name == "instructions" {
					got = p.Default
				}
			}
			if got != want {
				t.Errorf("default = %q, want %q", got, want)
			}

			// Only the one line changes — comments and everything else
			// survive, which is the whole reason this isn't a re-encode.
			before := strings.Split(string(raw), "\n")
			after := strings.Split(string(edited), "\n")
			if len(before) != len(after) {
				t.Fatalf("line count changed: %d -> %d", len(before), len(after))
			}
			diffs := 0
			for i := range before {
				if before[i] != after[i] {
					diffs++
				}
			}
			if diffs != 1 {
				t.Errorf("%d lines changed, want exactly 1", diffs)
			}
		})
	}
	if checked < 15 {
		t.Fatalf("only %d bots have an instructions port; expected every LLM bot to", checked)
	}
	t.Logf("editor verified against %d real bot files", checked)
}

func TestSetInstructionsDefaultRejectsABotWithoutThePort(t *testing.T) {
	// notify is deterministic — no ai.generate, so no instructions port.
	path := filepath.Join(repoRoot(t), "bots", "notify", "nanobot.yaml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := setInstructionsDefaultInYAML(raw, "anything"); err == nil {
		t.Error("expected an error rather than a silently unchanged file")
	}
}

func TestHandleSetBotInstructions(t *testing.T) {
	// A throwaway copy — this handler writes to the catalog.
	dir := t.TempDir()
	src := filepath.Join(repoRoot(t), "bots", "content-ideas")
	dst := filepath.Join(dir, "content-ideas")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(src, "nanobot.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dst, "nanobot.yaml"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	srv := &Server{BotsDir: dir}

	post := func(body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/bots/content-ideas/instructions", bytes.NewBufferString(body))
		srv.Handler().ServeHTTP(rec, req)
		return rec
	}

	rec := post(`{"instructions":"Only write about Go."}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	var summary BotSummary
	if err := json.Unmarshal(rec.Body.Bytes(), &summary); err != nil {
		t.Fatal(err)
	}
	var got string
	for _, p := range summary.Inputs {
		if p.Name == "instructions" {
			got = p.Default
		}
	}
	if got != "Only write about Go." {
		t.Errorf("returned summary has default %q, want the new text", got)
	}

	// A multi-line paste is refused rather than silently mangled into a
	// broken single-line scalar.
	if rec := post(`{"instructions":"line one\nline two"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("multi-line: status = %d, want 400 (body %s)", rec.Code, rec.Body)
	}
	// And something long enough to crowd out the bot's own rules.
	if rec := post(`{"instructions":"` + strings.Repeat("x", maxInstructionsLen+1) + `"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("over-long: status = %d, want 400", rec.Code)
	}
	// A deterministic bot has no port to set.
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/bots/nope/instructions",
		bytes.NewBufferString(`{"instructions":"x"}`)))
	if rec.Code == http.StatusOK {
		t.Error("a missing bot should not succeed")
	}
}
