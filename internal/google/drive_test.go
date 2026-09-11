package google

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestFilesGetReturnsMetadata(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/files/f1", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(DriveFile{ID: "f1", Name: "doc.pdf", WebViewLink: "https://drive/x"})
	})
	c := testClient(t, mux)
	f, err := c.FilesGet("f1")
	if err != nil {
		t.Fatalf("FilesGet: %v", err)
	}
	if f.Name != "doc.pdf" {
		t.Errorf("f = %+v", f)
	}
}

func TestResolveFolderIDWalksPathCreatingMissingSegments(t *testing.T) {
	mux := http.NewServeMux()
	var created []string
	mux.HandleFunc("/files", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			name := body["name"].(string)
			created = append(created, name)
			json.NewEncoder(w).Encode(map[string]string{"id": "id-" + name})
			return
		}
		q := r.URL.Query().Get("q")
		if strings.Contains(q, "'Recaps'") {
			// "Recaps" already exists
			json.NewEncoder(w).Encode(map[string]any{"files": []map[string]string{{"id": "recaps-id"}}})
			return
		}
		// "2026" doesn't exist yet
		json.NewEncoder(w).Encode(map[string]any{"files": []map[string]string{}})
	})
	c := testClient(t, mux)
	id, err := c.resolveFolderID("Recaps/2026")
	if err != nil {
		t.Fatalf("resolveFolderID: %v", err)
	}
	if id != "id-2026" {
		t.Errorf("id = %q, want id-2026", id)
	}
	if len(created) != 1 || created[0] != "2026" {
		t.Errorf("created = %v, want exactly [2026]", created)
	}
}

func TestFilesCreateUploadsMultipartWithMetadataAndData(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/files", func(w http.ResponseWriter, r *http.Request) {
		// folder resolution's GET /files (find-or-create "Recaps")
		json.NewEncoder(w).Encode(map[string]any{"files": []map[string]string{{"id": "folder-1"}}})
	})
	var gotContentType string
	var gotBody []byte
	mux.HandleFunc("/upload/files", func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		gotBody, _ = io.ReadAll(r.Body)
		json.NewEncoder(w).Encode(DriveFile{ID: "new-1", Name: "recap.pdf"})
	})
	srv := testClient(t, mux)
	// driveUploadBase points at the same test server but with a distinct
	// path prefix so the multipart upload handler doesn't collide with the
	// folder-resolution handler above.
	driveUploadBase = driveUploadBase + "/upload"

	f, err := srv.FilesCreate("Recaps", "recap.pdf", []byte("%PDF-fake"), "application/pdf")
	if err != nil {
		t.Fatalf("FilesCreate: %v", err)
	}
	if f.ID != "new-1" {
		t.Errorf("f = %+v", f)
	}
	if !strings.HasPrefix(gotContentType, "multipart/related") {
		t.Errorf("Content-Type = %q", gotContentType)
	}
	if !strings.Contains(string(gotBody), "%PDF-fake") {
		t.Error("expected the uploaded body to contain the file's actual bytes")
	}
}

func TestFilesDownloadReturnsBase64AndMime(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/files/f1", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("alt") == "media" {
			w.Write([]byte("hello file"))
			return
		}
		json.NewEncoder(w).Encode(DriveFile{ID: "f1", MimeType: "text/plain"})
	})
	c := testClient(t, mux)
	b64, mime, err := c.FilesDownload("f1")
	if err != nil {
		t.Fatalf("FilesDownload: %v", err)
	}
	if mime != "text/plain" {
		t.Errorf("mime = %q", mime)
	}
	if b64 == "" {
		t.Error("expected non-empty base64 content")
	}
}
