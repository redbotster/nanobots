package google

import (
	"fmt"
	"net/url"
)

var calendarBase = "https://www.googleapis.com/calendar/v3"

// Event is the shape bots/*/fixtures/*.json also uses for calendar.events.list
// fixtures — live and demo data share one shape.
type Event struct {
	ID        string   `json:"id"`
	Summary   string   `json:"summary"`
	Start     string   `json:"start"`
	End       string   `json:"end"`
	Attendees []string `json:"attendees,omitempty"`
	HTMLLink  string   `json:"html_link,omitempty"`
}

type rawEvent struct {
	ID      string `json:"id"`
	Summary string `json:"summary"`
	Start   struct {
		DateTime string `json:"dateTime"`
		Date     string `json:"date"`
	} `json:"start"`
	End struct {
		DateTime string `json:"dateTime"`
		Date     string `json:"date"`
	} `json:"end"`
	Attendees []struct {
		Email string `json:"email"`
	} `json:"attendees"`
	HTMLLink string `json:"htmlLink"`
}

func (r rawEvent) startOrAllDay() string {
	if r.Start.DateTime != "" {
		return r.Start.DateTime
	}
	return r.Start.Date
}

func (r rawEvent) endOrAllDay() string {
	if r.End.DateTime != "" {
		return r.End.DateTime
	}
	return r.End.Date
}

// EventsList implements `events.list`: every event on calendarID ("primary"
// for the connected account's main calendar) between timeMin and timeMax
// (RFC3339), ordered by start time.
func (c *Client) EventsList(calendarID, timeMin, timeMax string) ([]Event, error) {
	if calendarID == "" {
		calendarID = "primary"
	}
	listURL := fmt.Sprintf("%s/calendars/%s/events?timeMin=%s&timeMax=%s&singleEvents=true&orderBy=startTime",
		calendarBase, url.PathEscape(calendarID), url.QueryEscape(timeMin), url.QueryEscape(timeMax))
	var resp struct {
		Items []rawEvent `json:"items"`
	}
	if err := c.getJSON(listURL, &resp); err != nil {
		return nil, fmt.Errorf("events.list: %w", err)
	}
	out := make([]Event, 0, len(resp.Items))
	for _, it := range resp.Items {
		ev := Event{ID: it.ID, Summary: it.Summary, Start: it.startOrAllDay(), End: it.endOrAllDay(), HTMLLink: it.HTMLLink}
		for _, a := range it.Attendees {
			ev.Attendees = append(ev.Attendees, a.Email)
		}
		out = append(out, ev)
	}
	return out, nil
}
