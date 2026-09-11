package google

import (
	"encoding/base64"
	"fmt"
	"net/url"
	"strings"
)

var gmailBase = "https://gmail.googleapis.com/gmail/v1/users/me"

// Message is the shape internal/step's demo fixtures already use
// (gmail.messages.list.json in bots/recap-emails-to-pdf/fixtures, etc.) —
// this real implementation returns the same shape so a bot's prompts and
// downstream steps don't need to know whether they're on demo or live data.
type Message struct {
	ID      string `json:"id"`
	From    string `json:"from"`
	Subject string `json:"subject"`
	Date    string `json:"date"`
	Snippet string `json:"snippet"`
}

type gmailListResponse struct {
	Messages []struct {
		ID string `json:"id"`
	} `json:"messages"`
}

type gmailMessageResponse struct {
	Snippet string `json:"snippet"`
	Payload struct {
		Headers []struct{ Name, Value string } `json:"headers"`
	} `json:"payload"`
}

func headerValue(headers []struct{ Name, Value string }, name string) string {
	for _, h := range headers {
		if strings.EqualFold(h.Name, name) {
			return h.Value
		}
	}
	return ""
}

// MessagesList implements the `messages.list` op: a Gmail search query plus
// a result cap, returning message summaries (one extra API call per message
// to fetch its headers/snippet — Gmail's list endpoint alone only returns
// ids).
func (c *Client) MessagesList(q string, max int) ([]Message, error) {
	listURL := fmt.Sprintf("%s/messages?q=%s&maxResults=%d", gmailBase, url.QueryEscape(q), max)
	var list gmailListResponse
	if err := c.getJSON(listURL, &list); err != nil {
		return nil, fmt.Errorf("messages.list: %w", err)
	}
	out := make([]Message, 0, len(list.Messages))
	for _, m := range list.Messages {
		detailURL := fmt.Sprintf("%s/messages/%s?format=metadata&metadataHeaders=From&metadataHeaders=Subject&metadataHeaders=Date",
			gmailBase, m.ID)
		var detail gmailMessageResponse
		if err := c.getJSON(detailURL, &detail); err != nil {
			return nil, fmt.Errorf("messages.list: fetch %s: %w", m.ID, err)
		}
		out = append(out, Message{
			ID:      m.ID,
			From:    headerValue(detail.Payload.Headers, "From"),
			Subject: headerValue(detail.Payload.Headers, "Subject"),
			Date:    headerValue(detail.Payload.Headers, "Date"),
			Snippet: detail.Snippet,
		})
	}
	return out, nil
}

// buildRFC2822 constructs the minimal raw message Gmail's `raw` field wants:
// base64url, no padding.
func buildRFC2822(to, subject, body string) string {
	msg := fmt.Sprintf("To: %s\r\nSubject: %s\r\nContent-Type: text/plain; charset=\"UTF-8\"\r\n\r\n%s", to, subject, body)
	return base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString([]byte(msg))
}

type gmailSendResponse struct {
	ID string `json:"id"`
}

// MessagesSend implements `messages.send`, returning the sent message's id.
func (c *Client) MessagesSend(to, subject, body string) (string, error) {
	var resp gmailSendResponse
	err := c.postJSON(gmailBase+"/messages/send", map[string]string{"raw": buildRFC2822(to, subject, body)}, &resp)
	if err != nil {
		return "", fmt.Errorf("messages.send: %w", err)
	}
	return resp.ID, nil
}

type gmailDraftResponse struct {
	ID string `json:"id"`
}

// DraftsCreate implements `drafts.create` for one reply, returning the
// draft's id.
func (c *Client) DraftsCreate(to, subject, body string) (string, error) {
	var resp gmailDraftResponse
	req := map[string]any{
		"message": map[string]string{"raw": buildRFC2822(to, subject, body)},
	}
	if err := c.postJSON(gmailBase+"/drafts", req, &resp); err != nil {
		return "", fmt.Errorf("drafts.create: %w", err)
	}
	return resp.ID, nil
}

// DraftsSend implements `drafts.send`, returning the sent message's id —
// email-send-approved's one job, always behind its own unconditional gate.
func (c *Client) DraftsSend(draftID string) (string, error) {
	var resp gmailSendResponse
	err := c.postJSON(gmailBase+"/drafts/send", map[string]string{"id": draftID}, &resp)
	if err != nil {
		return "", fmt.Errorf("drafts.send: %w", err)
	}
	return resp.ID, nil
}

// ensureLabel finds a label by name or creates it, returning its id — Gmail
// has no "get or create" endpoint, so this is list-then-create like the
// vault/agent "ensure" pattern in internal/oneclaw.
func (c *Client) ensureLabel(name string) (string, error) {
	var list struct {
		Labels []struct{ ID, Name string } `json:"labels"`
	}
	if err := c.getJSON(gmailBase+"/labels", &list); err != nil {
		return "", fmt.Errorf("ensure label %q: list: %w", name, err)
	}
	for _, l := range list.Labels {
		if l.Name == name {
			return l.ID, nil
		}
	}
	var created struct {
		ID string `json:"id"`
	}
	req := map[string]string{"name": name, "labelListVisibility": "labelShow", "messageListVisibility": "show"}
	if err := c.postJSON(gmailBase+"/labels", req, &created); err != nil {
		return "", fmt.Errorf("ensure label %q: create: %w", name, err)
	}
	return created.ID, nil
}

// MessagesModify implements `messages.modify` as inbox-triage uses it:
// label every message in urgentIDs "URGENT" and every message in
// laterIDs "LATER". Label-only, per the catalog's design rule — this never
// archives or deletes.
func (c *Client) MessagesModify(urgentIDs, laterIDs []string) error {
	urgentLabel, err := c.ensureLabel("URGENT")
	if err != nil {
		return err
	}
	laterLabel, err := c.ensureLabel("LATER")
	if err != nil {
		return err
	}
	apply := func(ids []string, labelID string) error {
		for _, id := range ids {
			url := fmt.Sprintf("%s/messages/%s/modify", gmailBase, id)
			if err := c.postJSON(url, map[string]any{"addLabelIds": []string{labelID}}, nil); err != nil {
				return fmt.Errorf("messages.modify %s: %w", id, err)
			}
		}
		return nil
	}
	if err := apply(urgentIDs, urgentLabel); err != nil {
		return err
	}
	return apply(laterIDs, laterLabel)
}
