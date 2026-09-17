package step

import (
	"errors"
	"reflect"
	"testing"

	"github.com/redbotster/nanobots/internal/google"
)

// fakeGoogleAPI implements googleAPI in-memory so dispatchGoogle's op-mapping
// logic is tested without a real HTTP server or Google account.
type fakeGoogleAPI struct {
	messagesListResult                                 []google.Message
	messagesListErr                                    error
	sentTo, sentSubj, sentBody                         string
	modifiedUrgent, modifiedLater                      []string
	labelled                                           []string
	labelledWith                                       string
	draftsCreated                                      []struct{ to, subject, body string }
	draftSentID                                        string
	filesGetResult                                     *google.DriveFile
	filesListResult                                    []google.DriveFile
	filesCreateFolder, filesCreateFilename             string
	filesCreateData                                    []byte
	filesCreateMime                                    string
	filesCreateResult                                  *google.DriveFile
	downloadResult                                     string
	downloadMime                                       string
	rowsAppendSheet                                    string
	rowsAppendValues                                   []any
	rowsAppendResult                                   string
	eventsListCalendarID, eventsListMin, eventsListMax string
	eventsListResult                                   []google.Event
}

func (f *fakeGoogleAPI) MessagesList(q string, max int) ([]google.Message, error) {
	return f.messagesListResult, f.messagesListErr
}
func (f *fakeGoogleAPI) MessagesSend(to, subject, body string) (string, error) {
	f.sentTo, f.sentSubj, f.sentBody = to, subject, body
	return "sent-1", nil
}
func (f *fakeGoogleAPI) MessagesModify(urgentIDs, laterIDs []string) error {
	f.modifiedUrgent, f.modifiedLater = urgentIDs, laterIDs
	return nil
}
func (f *fakeGoogleAPI) AddLabel(ids []string, label string) error {
	f.labelled, f.labelledWith = ids, label
	return nil
}
func (f *fakeGoogleAPI) DraftsCreate(to, subject, body string) (string, error) {
	f.draftsCreated = append(f.draftsCreated, struct{ to, subject, body string }{to, subject, body})
	return "draft-" + to, nil
}
func (f *fakeGoogleAPI) DraftsSend(draftID string) (string, error) {
	f.draftSentID = draftID
	return "sent-" + draftID, nil
}
func (f *fakeGoogleAPI) FilesGet(id string) (*google.DriveFile, error) { return f.filesGetResult, nil }
func (f *fakeGoogleAPI) FilesList(folder string, max int) ([]google.DriveFile, error) {
	return f.filesListResult, nil
}
func (f *fakeGoogleAPI) FilesCreate(folder, filename string, data []byte, mimeType string) (*google.DriveFile, error) {
	f.filesCreateFolder, f.filesCreateFilename, f.filesCreateData, f.filesCreateMime = folder, filename, data, mimeType
	return f.filesCreateResult, nil
}
func (f *fakeGoogleAPI) FilesDownload(id string) (string, string, error) {
	return f.downloadResult, f.downloadMime, nil
}
func (f *fakeGoogleAPI) EventsList(calendarID, timeMin, timeMax string) ([]google.Event, error) {
	f.eventsListCalendarID, f.eventsListMin, f.eventsListMax = calendarID, timeMin, timeMax
	return f.eventsListResult, nil
}
func (f *fakeGoogleAPI) RowsAppend(sheetID string, values []any) (string, error) {
	f.rowsAppendSheet, f.rowsAppendValues = sheetID, values
	return f.rowsAppendResult, nil
}

func TestDispatchGoogleMessagesList(t *testing.T) {
	f := &fakeGoogleAPI{messagesListResult: []google.Message{{ID: "m1", Subject: "hi"}}}
	out, err := dispatchGoogle(f, "messages.list", map[string]any{"q": "after:x", "max": float64(50)}, nil)
	if err != nil {
		t.Fatalf("dispatchGoogle: %v", err)
	}
	// Must come back as []any/map[string]any (like a fixture's raw JSON), not
	// a typed []google.Message — lookupPath can't walk arbitrary structs, so
	// a typed result would silently break every `{{steps.x.output.field}}`.
	msgs, ok := out.([]any)
	if !ok || len(msgs) != 1 {
		t.Fatalf("out = %#v, want []any of length 1", out)
	}
	m, ok := msgs[0].(map[string]any)
	if !ok || m["id"] != "m1" {
		t.Errorf("msgs[0] = %#v", msgs[0])
	}
}

func TestDispatchGoogleMessagesListPropagatesError(t *testing.T) {
	f := &fakeGoogleAPI{messagesListErr: errors.New("boom")}
	_, err := dispatchGoogle(f, "messages.list", map[string]any{}, nil)
	if err == nil {
		t.Fatal("expected error to propagate")
	}
}

func TestDispatchGoogleMessagesSend(t *testing.T) {
	f := &fakeGoogleAPI{}
	out, err := dispatchGoogle(f, "messages.send", map[string]any{
		"to": "a@b.com", "subject": "s", "body": "b",
	}, nil)
	if err != nil {
		t.Fatalf("dispatchGoogle: %v", err)
	}
	if out != "sent-1" {
		t.Errorf("out = %v", out)
	}
	if f.sentTo != "a@b.com" || f.sentSubj != "s" || f.sentBody != "b" {
		t.Errorf("fake got %+v", f)
	}
}

func TestDispatchGoogleMessagesModifyExtractsIDsFromTriageOutput(t *testing.T) {
	f := &fakeGoogleAPI{}
	out, err := dispatchGoogle(f, "messages.modify", map[string]any{
		"urgent": []any{map[string]any{"id": "u1", "reason": "client"}},
		"later":  []any{map[string]any{"id": "l1"}, map[string]any{"id": "l2"}},
	}, nil)
	if err != nil {
		t.Fatalf("dispatchGoogle: %v", err)
	}
	if !reflect.DeepEqual(f.modifiedUrgent, []string{"u1"}) {
		t.Errorf("modifiedUrgent = %v", f.modifiedUrgent)
	}
	if !reflect.DeepEqual(f.modifiedLater, []string{"l1", "l2"}) {
		t.Errorf("modifiedLater = %v", f.modifiedLater)
	}
	m, ok := out.(map[string]any)
	if !ok || m["modified"] != 3 {
		t.Errorf("out = %#v, want modified=3", out)
	}
}

func TestDispatchGoogleDraftsCreateFansOutOverList(t *testing.T) {
	f := &fakeGoogleAPI{}
	out, err := dispatchGoogle(f, "drafts.create", map[string]any{
		"drafts": []any{
			map[string]any{"thread_id": "t1", "to": "a@b.com", "subject": "Re: a", "body": "..."},
			map[string]any{"thread_id": "t2", "to": "c@d.com", "subject": "Re: c", "body": "..."},
		},
	}, nil)
	if err != nil {
		t.Fatalf("dispatchGoogle: %v", err)
	}
	if len(f.draftsCreated) != 2 {
		t.Fatalf("expected 2 DraftsCreate calls, got %d", len(f.draftsCreated))
	}
	m, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("out = %#v", out)
	}
	ids, ok := m["draft_ids"].([]string)
	if !ok || len(ids) != 2 {
		t.Errorf("draft_ids = %#v", m["draft_ids"])
	}
}

func TestDispatchGoogleFilesListReturnsSingleObjectNotArray(t *testing.T) {
	f := &fakeGoogleAPI{filesListResult: []google.DriveFile{{ID: "f1", Name: "a"}, {ID: "f2", Name: "b"}}}
	out, err := dispatchGoogle(f, "files.list", map[string]any{"folder": "Posts"}, nil)
	if err != nil {
		t.Fatalf("dispatchGoogle: %v", err)
	}
	file, ok := out.(map[string]any)
	if !ok || file["id"] != "f1" {
		t.Errorf("out = %#v, want the first (most recent) file as a bare map[string]any object", out)
	}
}

func TestDispatchGoogleFilesListErrorsWhenEmpty(t *testing.T) {
	f := &fakeGoogleAPI{}
	if _, err := dispatchGoogle(f, "files.list", map[string]any{"folder": "Posts"}, nil); err == nil {
		t.Fatal("expected an error for an empty folder")
	}
}

// fakeBlobStore lets resolveFileParam's blob read be tested without a real
// filesystem-backed store.
type fakeBlobStore struct {
	byURI map[string][]byte
}

func (b *fakeBlobStore) Write(data []byte, mime string) (FileValue, error) { return FileValue{}, nil }
func (b *fakeBlobStore) Read(uri string) ([]byte, error)                   { return b.byURI[uri], nil }

func TestDispatchGoogleFilesCreateResolvesFileValueFromBlobStore(t *testing.T) {
	blobs := &fakeBlobStore{byURI: map[string][]byte{"nbf://sha256/abc": []byte("%PDF-fake")}}
	f := &fakeGoogleAPI{filesCreateResult: &google.DriveFile{ID: "new-1"}}
	out, err := dispatchGoogle(f, "files.create", map[string]any{
		"folder": "Recaps/2026",
		"file":   FileValue{URI: "nbf://sha256/abc", Mime: "application/pdf"},
	}, blobs)
	if err != nil {
		t.Fatalf("dispatchGoogle: %v", err)
	}
	if f.filesCreateFolder != "Recaps/2026" || string(f.filesCreateData) != "%PDF-fake" || f.filesCreateMime != "application/pdf" {
		t.Errorf("fake got %+v", f)
	}
	if got, ok := out.(map[string]any); !ok || got["id"] != "new-1" {
		t.Errorf("out = %#v", out)
	}
}

func TestDispatchGoogleFilesCreateAcceptsJSONDecodedFileValue(t *testing.T) {
	blobs := &fakeBlobStore{byURI: map[string][]byte{"nbf://sha256/abc": []byte("data")}}
	f := &fakeGoogleAPI{filesCreateResult: &google.DriveFile{}}
	_, err := dispatchGoogle(f, "files.create", map[string]any{
		"folder": "F",
		"file":   map[string]any{"uri": "nbf://sha256/abc", "mime": "text/plain"},
	}, blobs)
	if err != nil {
		t.Fatalf("dispatchGoogle: %v", err)
	}
	if f.filesCreateMime != "text/plain" {
		t.Errorf("mime = %q", f.filesCreateMime)
	}
}

func TestDispatchGoogleFilesDownloadReturnsContentBase64AndMime(t *testing.T) {
	f := &fakeGoogleAPI{downloadResult: "aGVsbG8=", downloadMime: "text/markdown"}
	out, err := dispatchGoogle(f, "files.download", map[string]any{"id": "f1"}, nil)
	if err != nil {
		t.Fatalf("dispatchGoogle: %v", err)
	}
	m, ok := out.(map[string]any)
	if !ok || m["content_base64"] != "aGVsbG8=" || m["mime"] != "text/markdown" {
		t.Errorf("out = %#v", out)
	}
}

func TestDispatchGoogleRowsAppendFlattensValuesInSortedKeyOrder(t *testing.T) {
	f := &fakeGoogleAPI{rowsAppendResult: "42"}
	out, err := dispatchGoogle(f, "rows.append", map[string]any{
		"sheet":  "sheet-1",
		"values": map[string]any{"name": "Jamie", "email": "j@x.com", "company": "Acme"},
	}, nil)
	if err != nil {
		t.Fatalf("dispatchGoogle: %v", err)
	}
	want := []any{"Acme", "j@x.com", "Jamie"} // company, email, name — alphabetical
	if !reflect.DeepEqual(f.rowsAppendValues, want) {
		t.Errorf("rowsAppendValues = %#v, want %#v", f.rowsAppendValues, want)
	}
	m, ok := out.(map[string]any)
	if !ok || m["row_number"] != "42" {
		t.Errorf("out = %#v", out)
	}
}

func TestDispatchGoogleEventsListPassesParamsAndShapesResult(t *testing.T) {
	f := &fakeGoogleAPI{eventsListResult: []google.Event{{ID: "e1", Summary: "1:1", Start: "2026-09-11T10:00:00-05:00"}}}
	out, err := dispatchGoogle(f, "events.list", map[string]any{
		"calendar_id": "primary", "time_min": "2026-09-11T00:00:00Z", "time_max": "2026-09-12T00:00:00Z",
	}, nil)
	if err != nil {
		t.Fatalf("dispatchGoogle: %v", err)
	}
	if f.eventsListCalendarID != "primary" || f.eventsListMin != "2026-09-11T00:00:00Z" || f.eventsListMax != "2026-09-12T00:00:00Z" {
		t.Errorf("fake got calendarID=%q min=%q max=%q", f.eventsListCalendarID, f.eventsListMin, f.eventsListMax)
	}
	items, ok := out.([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("out = %#v, want []any of length 1", out)
	}
	m, ok := items[0].(map[string]any)
	if !ok || m["summary"] != "1:1" {
		t.Errorf("items[0] = %#v", items[0])
	}
}

func TestDispatchGoogleUnsupportedOpErrors(t *testing.T) {
	if _, err := dispatchGoogle(&fakeGoogleAPI{}, "nonsense.op", nil, nil); err == nil {
		t.Fatal("expected an error for an unrecognized op")
	}
}

func TestToJSONAnyProducesMapWalkableByLookupPath(t *testing.T) {
	out, err := toJSONAny(&google.DriveFile{ID: "f1", Name: "a.pdf", WebViewLink: "https://x"})
	if err != nil {
		t.Fatalf("toJSONAny: %v", err)
	}
	ctx := map[string]any{"steps": map[string]any{"lookup": map[string]any{"output": out}}}
	got, ok := lookupPath(ctx, "steps.lookup.output.webViewLink")
	if !ok || got != "https://x" {
		t.Errorf("lookupPath = %v, %v, want https://x, true", got, ok)
	}
}

func TestParamIntFallsBackOnMissingOrWrongType(t *testing.T) {
	if got := paramInt(nil, 7); got != 7 {
		t.Errorf("paramInt(nil, 7) = %d", got)
	}
	if got := paramInt("not a number", 7); got != 7 {
		t.Errorf("paramInt(string, 7) = %d", got)
	}
	if got := paramInt(float64(3), 7); got != 3 {
		t.Errorf("paramInt(3.0, 7) = %d", got)
	}
}

// messages.label is how follow-up-chaser records that it has nudged a
// thread, so the next run's `-label:` query excludes it. Labelling nothing
// and reporting success is the failure that matters: it turns a bot that
// nudges someone once into one that nudges them every weekday for ever.
func TestDispatchGoogleLabelsMessages(t *testing.T) {
	for _, tc := range []struct {
		name string
		ids  any
		want []string
	}{
		{
			name: "a list<json> port, as stale_threads arrives",
			ids:  []any{map[string]any{"id": "18f301", "subject": "Proposal"}},
			want: []string{"18f301"},
		},
		{
			// Silently dropped before, which is the quiet version of the bug.
			name: "a list<string> port",
			ids:  []any{"18f301", "18f302"},
			want: []string{"18f301", "18f302"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeGoogleAPI{}
			out, err := dispatchGoogle(f, "messages.label", map[string]any{
				"ids": tc.ids, "label": "nanobots-nudged",
			}, nil)
			if err != nil {
				t.Fatalf("dispatchGoogle: %v", err)
			}
			if !reflect.DeepEqual(f.labelled, tc.want) {
				t.Errorf("labelled %v, want %v", f.labelled, tc.want)
			}
			if f.labelledWith != "nanobots-nudged" {
				t.Errorf("label = %q", f.labelledWith)
			}
			if m := out.(map[string]any); m["labelled"] != len(tc.want) {
				t.Errorf("reported %v labelled, did %d", m["labelled"], len(tc.want))
			}
		})
	}
}

// An empty label would put nothing anywhere while reporting success.
func TestDispatchGoogleLabelRefusesAnEmptyLabel(t *testing.T) {
	if _, err := dispatchGoogle(&fakeGoogleAPI{}, "messages.label",
		map[string]any{"ids": []any{"1"}, "label": "  "}, nil); err == nil {
		t.Fatal("an empty label was accepted")
	}
}
