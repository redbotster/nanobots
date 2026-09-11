package step

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/redbotster/nanobots/internal/schema"
)

func TestFetchURLStripsHTMLToPlainText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><head><style>body{color:red}</style></head><body><h1>Q3 Launch</h1><p>We shipped it.</p><script>evil()</script></body></html>`))
	}))
	defer srv.Close()

	page, err := fetchURL(srv.URL)
	if err != nil {
		t.Fatalf("fetchURL: %v", err)
	}
	if page.URL != srv.URL {
		t.Errorf("URL = %q", page.URL)
	}
	want := "Q3 Launch We shipped it."
	if page.Text != want {
		t.Errorf("Text = %q, want %q", page.Text, want)
	}
}

func TestFetchURLReturnsErrorOnHTTPFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	if _, err := fetchURL(srv.URL); err == nil {
		t.Fatal("expected an error for a 404 response")
	}
}

func TestRunWebFetchSingleURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("<p>hello</p>"))
	}))
	defer srv.Close()

	out, err := runWebFetch(map[string]any{"url": srv.URL})
	if err != nil {
		t.Fatalf("runWebFetch: %v", err)
	}
	m, ok := out.(map[string]any)
	if !ok || m["text"] != "hello" {
		t.Errorf("out = %#v", out)
	}
}

func TestRunWebFetchMultipleURLs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("<p>page</p>"))
	}))
	defer srv.Close()

	out, err := runWebFetch(map[string]any{"urls": []any{srv.URL, srv.URL}})
	if err != nil {
		t.Fatalf("runWebFetch: %v", err)
	}
	items, ok := out.([]any)
	if !ok || len(items) != 2 {
		t.Fatalf("out = %#v, want []any of length 2", out)
	}
}

func TestRunWebFetchRequiresURLOrURLs(t *testing.T) {
	if _, err := runWebFetch(map[string]any{}); err == nil {
		t.Fatal("expected an error when neither url nor urls is given")
	}
}

// TestInterpretWebFetchStep proves the interpreter wires a `web.fetch` step
// through deps.WebFetch (so Demo/Live/Remote can each decide fixture vs.
// real fetch) — the real HTTP-fetching/HTML-stripping logic itself is
// covered directly by TestFetchURL*/TestRunWebFetch* above.
func TestInterpretWebFetchStep(t *testing.T) {
	fake := &fakeDeps{serviceResult: map[string]any{"url": "https://example.com", "text": "from deps.WebFetch"}}
	nb := simpleBot(
		[]schema.Step{{
			Name: "fetch", Type: "web.fetch",
			Params: map[string]any{"url": "https://example.com"},
			Output: "page",
		}},
		[]schema.OutputPort{{Name: "page", Type: "json"}},
	)
	res, err := Interpret(nb, map[string]any{}, nil, fake)
	if err != nil {
		t.Fatalf("Interpret: %v", err)
	}
	page, ok := res.Outputs["page"].(map[string]any)
	if !ok || page["text"] != "from deps.WebFetch" {
		t.Errorf("page = %#v", res.Outputs["page"])
	}
}
