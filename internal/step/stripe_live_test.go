package step

import (
	"errors"
	"testing"

	"github.com/redbotster/nanobots/internal/stripe"
)

type fakeStripeAPI struct {
	result    []stripe.Invoice
	err       error
	gotStatus string
	gotMax    int
}

func (f *fakeStripeAPI) InvoicesList(status string, max int) ([]stripe.Invoice, error) {
	f.gotStatus, f.gotMax = status, max
	return f.result, f.err
}

func TestDispatchStripeInvoicesListShapesResultAsWalkableJSON(t *testing.T) {
	f := &fakeStripeAPI{result: []stripe.Invoice{{ID: "in_1", AmountDue: 4500}}}
	out, err := dispatchStripe(f, "invoices.list", map[string]any{"status": "open", "max": float64(5)})
	if err != nil {
		t.Fatalf("dispatchStripe: %v", err)
	}
	if f.gotStatus != "open" || f.gotMax != 5 {
		t.Errorf("fake got status=%q max=%d", f.gotStatus, f.gotMax)
	}
	items, ok := out.([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("out = %#v, want []any of length 1", out)
	}
	m, ok := items[0].(map[string]any)
	if !ok || m["id"] != "in_1" {
		t.Errorf("items[0] = %#v", items[0])
	}
}

func TestDispatchStripePropagatesError(t *testing.T) {
	f := &fakeStripeAPI{err: errors.New("boom")}
	if _, err := dispatchStripe(f, "invoices.list", map[string]any{}); err == nil {
		t.Fatal("expected error to propagate")
	}
}

func TestDispatchStripeUnsupportedOpErrors(t *testing.T) {
	if _, err := dispatchStripe(&fakeStripeAPI{}, "nonsense.op", nil); err == nil {
		t.Fatal("expected an error for an unrecognized op")
	}
}

func TestStripeClientErrorsWhenNotConfigured(t *testing.T) {
	l := &LiveDeps{}
	if _, err := l.stripeClient(); err == nil {
		t.Fatal("expected an error when StripeConfig is unset")
	}
}
