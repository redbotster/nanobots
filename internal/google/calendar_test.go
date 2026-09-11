package google

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestEventsListParsesTimedAndAllDayEvents(t *testing.T) {
	var gotPath string
	mux := http.NewServeMux()
	mux.HandleFunc("/calendars/primary/events", func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		json.NewEncoder(w).Encode(map[string]any{
			"items": []map[string]any{
				{
					"id": "e1", "summary": "1:1 with Priya",
					"start": map[string]string{"dateTime": "2026-09-11T10:00:00-05:00"},
					"end":   map[string]string{"dateTime": "2026-09-11T10:30:00-05:00"},
					"attendees": []map[string]string{
						{"email": "priya@northwind-consulting.com"},
					},
					"htmlLink": "https://calendar.google.com/e1",
				},
				{
					"id": "e2", "summary": "Company holiday",
					"start": map[string]string{"date": "2026-09-11"},
					"end":   map[string]string{"date": "2026-09-12"},
				},
			},
		})
	})
	c := testClient(t, mux)

	events, err := c.EventsList("", "2026-09-11T00:00:00Z", "2026-09-12T00:00:00Z")
	if err != nil {
		t.Fatalf("EventsList: %v", err)
	}
	if gotPath != "/calendars/primary/events" {
		t.Errorf("path = %q, want default calendarID to resolve to 'primary'", gotPath)
	}
	if len(events) != 2 {
		t.Fatalf("events = %+v", events)
	}
	if events[0].Start != "2026-09-11T10:00:00-05:00" || len(events[0].Attendees) != 1 {
		t.Errorf("events[0] = %+v", events[0])
	}
	if events[1].Start != "2026-09-11" {
		t.Errorf("events[1] (all-day) = %+v, want Start=2026-09-11 (date fallback)", events[1])
	}
}
