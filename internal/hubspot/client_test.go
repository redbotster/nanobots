package hubspot

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func testClient(t *testing.T, mux *http.ServeMux) *Client {
	t.Helper()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	orig := apiBase
	apiBase = srv.URL
	t.Cleanup(func() { apiBase = orig })
	return NewClient("pat-test-123")
}

func TestContactSearchFindsExistingContact(t *testing.T) {
	var gotAuth string
	mux := http.NewServeMux()
	mux.HandleFunc("/crm/v3/objects/contacts/search", func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		json.NewEncoder(w).Encode(map[string]any{
			"results": []map[string]any{
				{"id": "c1", "properties": map[string]string{"email": "lead@example.com", "firstname": "Jamie"}},
			},
		})
	})
	c := testClient(t, mux)

	contact, err := c.ContactSearch("lead@example.com")
	if err != nil {
		t.Fatalf("ContactSearch: %v", err)
	}
	if contact == nil || contact.ID != "c1" {
		t.Fatalf("contact = %+v", contact)
	}
	if gotAuth != "Bearer pat-test-123" {
		t.Errorf("Authorization = %q", gotAuth)
	}
}

func TestContactSearchReturnsNilWhenNotFound(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/crm/v3/objects/contacts/search", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"results": []map[string]any{}})
	})
	c := testClient(t, mux)

	contact, err := c.ContactSearch("nobody@example.com")
	if err != nil {
		t.Fatalf("ContactSearch: %v", err)
	}
	if contact != nil {
		t.Errorf("contact = %+v, want nil for no match", contact)
	}
}

func TestContactUpsertCreatesWhenNoneExists(t *testing.T) {
	var gotMethod, gotPath string
	mux := http.NewServeMux()
	mux.HandleFunc("/crm/v3/objects/contacts/search", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"results": []map[string]any{}})
	})
	mux.HandleFunc("/crm/v3/objects/contacts", func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		json.NewEncoder(w).Encode(map[string]any{"id": "c-new", "properties": map[string]string{"email": "new@example.com"}})
	})
	c := testClient(t, mux)

	contact, err := c.ContactUpsert(map[string]string{"email": "new@example.com", "firstname": "Alex"})
	if err != nil {
		t.Fatalf("ContactUpsert: %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/crm/v3/objects/contacts" {
		t.Errorf("method=%s path=%s, want POST /crm/v3/objects/contacts", gotMethod, gotPath)
	}
	if contact.ID != "c-new" {
		t.Errorf("contact = %+v", contact)
	}
}

func TestContactUpsertUpdatesWhenAlreadyExists(t *testing.T) {
	var gotMethod, gotPath string
	mux := http.NewServeMux()
	mux.HandleFunc("/crm/v3/objects/contacts/search", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"results": []map[string]any{{"id": "c1", "properties": map[string]string{"email": "lead@example.com"}}},
		})
	})
	mux.HandleFunc("/crm/v3/objects/contacts/c1", func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		json.NewEncoder(w).Encode(map[string]any{"id": "c1", "properties": map[string]string{"email": "lead@example.com", "fit_score": "high"}})
	})
	c := testClient(t, mux)

	contact, err := c.ContactUpsert(map[string]string{"email": "lead@example.com", "fit_score": "high"})
	if err != nil {
		t.Fatalf("ContactUpsert: %v", err)
	}
	if gotMethod != http.MethodPatch || gotPath != "/crm/v3/objects/contacts/c1" {
		t.Errorf("method=%s path=%s, want PATCH .../c1", gotMethod, gotPath)
	}
	if contact.Properties["fit_score"] != "high" {
		t.Errorf("contact = %+v", contact)
	}
}

func TestContactUpsertRejectsMissingEmail(t *testing.T) {
	c := testClient(t, http.NewServeMux())
	if _, err := c.ContactUpsert(map[string]string{"firstname": "no email"}); err == nil {
		t.Fatal("expected an error when properties.email is missing")
	}
}
