package google

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
	origGmail, origDrive, origUpload, origSheets := gmailBase, driveBase, driveUploadBase, sheetsBase
	gmailBase, driveBase, driveUploadBase, sheetsBase = srv.URL, srv.URL, srv.URL, srv.URL
	t.Cleanup(func() {
		gmailBase, driveBase, driveUploadBase, sheetsBase = origGmail, origDrive, origUpload, origSheets
	})
	return NewClient(func() (string, error) { return "test-token", nil })
}

func TestMessagesListFetchesDetailsPerMessage(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/messages", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("Authorization = %q", got)
		}
		json.NewEncoder(w).Encode(map[string]any{"messages": []map[string]string{{"id": "m1"}, {"id": "m2"}}})
	})
	mux.HandleFunc("/messages/m1", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"snippet": "snippet one",
			"payload": map[string]any{"headers": []map[string]string{
				{"Name": "From", "Value": "a@example.com"}, {"Name": "Subject", "Value": "Subj 1"},
			}},
		})
	})
	mux.HandleFunc("/messages/m2", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"snippet": "snippet two",
			"payload": map[string]any{"headers": []map[string]string{
				{"Name": "From", "Value": "b@example.com"}, {"Name": "Subject", "Value": "Subj 2"},
			}},
		})
	})
	c := testClient(t, mux)
	msgs, err := c.MessagesList("label:INBOX", 10)
	if err != nil {
		t.Fatalf("MessagesList: %v", err)
	}
	if len(msgs) != 2 || msgs[0].From != "a@example.com" || msgs[1].Subject != "Subj 2" {
		t.Errorf("msgs = %+v", msgs)
	}
}

func TestMessagesSendReturnsID(t *testing.T) {
	mux := http.NewServeMux()
	var gotBody map[string]string
	mux.HandleFunc("/messages/send", func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&gotBody)
		json.NewEncoder(w).Encode(map[string]string{"id": "sent-1"})
	})
	c := testClient(t, mux)
	id, err := c.MessagesSend("to@example.com", "Hi", "body")
	if err != nil {
		t.Fatalf("MessagesSend: %v", err)
	}
	if id != "sent-1" {
		t.Errorf("id = %q", id)
	}
	if gotBody["raw"] == "" {
		t.Error("expected a non-empty raw message")
	}
}

func TestEnsureLabelReusesExisting(t *testing.T) {
	mux := http.NewServeMux()
	createCalls := 0
	mux.HandleFunc("/labels", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			createCalls++
			json.NewEncoder(w).Encode(map[string]string{"id": "new-label"})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"labels": []map[string]string{{"id": "existing-id", "name": "URGENT"}},
		})
	})
	c := testClient(t, mux)
	id, err := c.ensureLabel("URGENT")
	if err != nil {
		t.Fatalf("ensureLabel: %v", err)
	}
	if id != "existing-id" {
		t.Errorf("id = %q, want existing-id", id)
	}
	if createCalls != 0 {
		t.Errorf("expected no create call for an existing label, got %d", createCalls)
	}
}

func TestMessagesModifyAppliesCorrectLabels(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/labels", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"labels": []map[string]string{
			{"id": "urgent-id", "name": "URGENT"}, {"id": "later-id", "name": "LATER"},
		}})
	})
	var modified []struct {
		ID     string
		Labels []string
	}
	mux.HandleFunc("/messages/", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			AddLabelIds []string `json:"addLabelIds"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		id := r.URL.Path[len("/messages/") : len(r.URL.Path)-len("/modify")]
		modified = append(modified, struct {
			ID     string
			Labels []string
		}{id, body.AddLabelIds})
		w.Write([]byte("{}"))
	})
	c := testClient(t, mux)
	if err := c.MessagesModify([]string{"u1"}, []string{"l1", "l2"}); err != nil {
		t.Fatalf("MessagesModify: %v", err)
	}
	if len(modified) != 3 {
		t.Fatalf("expected 3 modify calls (1 urgent + 2 later), got %d", len(modified))
	}
	if modified[0].ID != "u1" || modified[0].Labels[0] != "urgent-id" {
		t.Errorf("modified[0] = %+v", modified[0])
	}
}
