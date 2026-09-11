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

func TestInterpretWebFetchStep(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("<p>from the interpreter</p>"))
	}))
	defer srv.Close()

	nb := simpleBot(
		[]schema.Step{{
			Name: "fetch", Type: "web.fetch",
			Params: map[string]any{"url": srv.URL},
			Output: "page",
		}},
		[]schema.OutputPort{{Name: "page", Type: "json"}},
	)
	res, err := Interpret(nb, map[string]any{}, nil, &fakeDeps{})
	if err != nil {
		t.Fatalf("Interpret: %v", err)
	}
	page, ok := res.Outputs["page"].(map[string]any)
	if !ok || page["text"] != "from the interpreter" {
		t.Errorf("page = %#v", res.Outputs["page"])
	}
}
