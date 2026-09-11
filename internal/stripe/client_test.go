package stripe

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
	return NewClient("sk_test_123")
}

func TestInvoicesListSendsStatusAndAuth(t *testing.T) {
	var gotAuth, gotQuery string
	mux := http.NewServeMux()
	mux.HandleFunc("/invoices", func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotQuery = r.URL.RawQuery
		json.NewEncoder(w).Encode(invoicesListResponse{Data: []Invoice{
			{ID: "in_1", CustomerEmail: "client@example.com", AmountDue: 45000, DueDate: 1893456000, HostedInvoiceURL: "https://invoice.stripe.com/i/in_1", Status: "open"},
		}})
	})
	c := testClient(t, mux)

	invoices, err := c.InvoicesList("open", 10)
	if err != nil {
		t.Fatalf("InvoicesList: %v", err)
	}
	if gotAuth != "Bearer sk_test_123" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if gotQuery != "limit=10&status=open" {
		t.Errorf("query = %q", gotQuery)
	}
	if len(invoices) != 1 || invoices[0].AmountDue != 45000 {
		t.Errorf("invoices = %+v", invoices)
	}
}

func TestInvoicesListReturnsErrorOnHTTPFailure(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/invoices", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	c := testClient(t, mux)

	if _, err := c.InvoicesList("open", 10); err == nil {
		t.Fatal("expected an error for a 401 response")
	}
}
