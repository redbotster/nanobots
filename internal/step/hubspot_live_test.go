package step

import (
	"errors"
	"testing"

	"github.com/redbotster/nanobots/internal/hubspot"
)

type fakeHubSpotAPI struct {
	searchResult *hubspot.Contact
	searchErr    error
	upsertResult *hubspot.Contact
	upsertErr    error
	gotEmail     string
	gotProps     map[string]string
}

func (f *fakeHubSpotAPI) ContactSearch(email string) (*hubspot.Contact, error) {
	f.gotEmail = email
	return f.searchResult, f.searchErr
}
func (f *fakeHubSpotAPI) ContactUpsert(properties map[string]string) (*hubspot.Contact, error) {
	f.gotProps = properties
	return f.upsertResult, f.upsertErr
}

func TestDispatchHubSpotContactsSearchFound(t *testing.T) {
	f := &fakeHubSpotAPI{searchResult: &hubspot.Contact{ID: "c1", Email: "lead@example.com"}}
	out, err := dispatchHubSpot(f, "contacts.search", map[string]any{"email": "lead@example.com"})
	if err != nil {
		t.Fatalf("dispatchHubSpot: %v", err)
	}
	if f.gotEmail != "lead@example.com" {
		t.Errorf("gotEmail = %q", f.gotEmail)
	}
	m, ok := out.(map[string]any)
	if !ok || m["id"] != "c1" {
		t.Errorf("out = %#v", out)
	}
}

func TestDispatchHubSpotContactsSearchNotFoundReturnsNilNotError(t *testing.T) {
	f := &fakeHubSpotAPI{searchResult: nil}
	out, err := dispatchHubSpot(f, "contacts.search", map[string]any{"email": "nobody@example.com"})
	if err != nil {
		t.Fatalf("dispatchHubSpot: %v", err)
	}
	if out != nil {
		t.Errorf("out = %#v, want nil for no match", out)
	}
}

func TestDispatchHubSpotContactsUpsertPassesStringPropertiesOnly(t *testing.T) {
	f := &fakeHubSpotAPI{upsertResult: &hubspot.Contact{ID: "c2", Email: "new@example.com"}}
	out, err := dispatchHubSpot(f, "contacts.upsert", map[string]any{
		"email": "new@example.com", "fit_score": "high", "ignored_non_string": float64(42),
	})
	if err != nil {
		t.Fatalf("dispatchHubSpot: %v", err)
	}
	if f.gotProps["email"] != "new@example.com" || f.gotProps["fit_score"] != "high" {
		t.Errorf("gotProps = %+v", f.gotProps)
	}
	if _, ok := f.gotProps["ignored_non_string"]; ok {
		t.Error("expected a non-string param to be dropped, not passed through")
	}
	m, ok := out.(map[string]any)
	if !ok || m["id"] != "c2" {
		t.Errorf("out = %#v", out)
	}
}

func TestDispatchHubSpotPropagatesError(t *testing.T) {
	f := &fakeHubSpotAPI{searchErr: errors.New("boom")}
	if _, err := dispatchHubSpot(f, "contacts.search", map[string]any{"email": "x@example.com"}); err == nil {
		t.Fatal("expected error to propagate")
	}
}

func TestDispatchHubSpotUnsupportedOpErrors(t *testing.T) {
	if _, err := dispatchHubSpot(&fakeHubSpotAPI{}, "nonsense.op", nil); err == nil {
		t.Fatal("expected an error for an unrecognized op")
	}
}

func TestHubSpotClientErrorsWhenNotConfigured(t *testing.T) {
	l := &LiveDeps{}
	if _, err := l.hubspotClient(); err == nil {
		t.Fatal("expected an error when HubSpotConfig is unset")
	}
}
