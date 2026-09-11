package google

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
)

var driveBase = "https://www.googleapis.com/drive/v3"
var driveUploadBase = "https://www.googleapis.com/upload/drive/v3"

// DriveFile is the shape used across bots/*/fixtures/gdrive.files.*.json —
// again, matching the demo shape exactly so nothing downstream needs to
// know whether it's on real or fixture data.
type DriveFile struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	WebViewLink string `json:"webViewLink,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
	Size        string `json:"size,omitempty"`
	CreatedTime string `json:"createdTime,omitempty"`
}

// resolveFolderID turns a human-readable folder path segment (bots declare
// e.g. "Recaps/2026", matching how someone would type it in Drive's search
// box) into a Drive folder id, creating intermediate folders as needed —
// Drive has no real path concept, just parent-child links, so "a folder
// named X inside a folder named Y" has to be walked one segment at a time.
func (c *Client) resolveFolderID(path string) (string, error) {
	parent := "root"
	for _, name := range strings.Split(strings.Trim(path, "/"), "/") {
		if name == "" {
			continue
		}
		id, err := c.findOrCreateFolder(name, parent)
		if err != nil {
			return "", err
		}
		parent = id
	}
	return parent, nil
}

func (c *Client) findOrCreateFolder(name, parentID string) (string, error) {
	q := fmt.Sprintf("name=%s and '%s' in parents and mimeType='application/vnd.google-apps.folder' and trashed=false",
		quoteForDriveQuery(name), parentID)
	var list struct {
		Files []struct {
			ID string `json:"id"`
		} `json:"files"`
	}
	listURL := fmt.Sprintf("%s/files?q=%s&fields=files(id)", driveBase, url.QueryEscape(q))
	if err := c.getJSON(listURL, &list); err != nil {
		return "", fmt.Errorf("find folder %q: %w", name, err)
	}
	if len(list.Files) > 0 {
		return list.Files[0].ID, nil
	}
	var created struct {
		ID string `json:"id"`
	}
	req := map[string]any{"name": name, "mimeType": "application/vnd.google-apps.folder", "parents": []string{parentID}}
	if err := c.postJSON(driveBase+"/files", req, &created); err != nil {
		return "", fmt.Errorf("create folder %q: %w", name, err)
	}
	return created.ID, nil
}

// quoteForDriveQuery quotes a string for use inside a Drive `q` search
// expression (single-quoted, with embedded single quotes escaped).
func quoteForDriveQuery(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "\\'") + "'"
}

// FilesGet implements `files.get`.
func (c *Client) FilesGet(id string) (*DriveFile, error) {
	var f DriveFile
	url := fmt.Sprintf("%s/files/%s?fields=id,name,webViewLink,mimeType,size,createdTime", driveBase, id)
	if err := c.getJSON(url, &f); err != nil {
		return nil, fmt.Errorf("files.get: %w", err)
	}
	return &f, nil
}

// FilesList implements `files.list`: the most recent file in a folder.
func (c *Client) FilesList(folder string, max int) ([]DriveFile, error) {
	folderID, err := c.resolveFolderID(folder)
	if err != nil {
		return nil, fmt.Errorf("files.list: %w", err)
	}
	q := fmt.Sprintf("'%s' in parents and trashed=false", folderID)
	listURL := fmt.Sprintf("%s/files?q=%s&orderBy=createdTime desc&pageSize=%d&fields=files(id,name,mimeType,createdTime)",
		driveBase, url.QueryEscape(q), max)
	var resp struct {
		Files []DriveFile `json:"files"`
	}
	if err := c.getJSON(listURL, &resp); err != nil {
		return nil, fmt.Errorf("files.list: %w", err)
	}
	return resp.Files, nil
}

// FilesCreate implements `files.create`: uploads data into folder (creating
// any missing folder path segments), returning the new file's metadata.
func (c *Client) FilesCreate(folder, filename string, data []byte, mimeType string) (*DriveFile, error) {
	folderID, err := c.resolveFolderID(folder)
	if err != nil {
		return nil, fmt.Errorf("files.create: %w", err)
	}
	metadata, _ := json.Marshal(map[string]any{"name": filename, "parents": []string{folderID}})

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	metaPart, _ := writer.CreatePart(map[string][]string{"Content-Type": {"application/json; charset=UTF-8"}})
	metaPart.Write(metadata)
	mediaPart, _ := writer.CreatePart(map[string][]string{"Content-Type": {mimeType}})
	mediaPart.Write(data)
	writer.Close()

	raw, err := c.do(http.MethodPost,
		driveUploadBase+"/files?uploadType=multipart&fields=id,name,webViewLink",
		body.Bytes(), "multipart/related; boundary="+writer.Boundary())
	if err != nil {
		return nil, fmt.Errorf("files.create: %w", err)
	}
	var f DriveFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("files.create: parse response: %w", err)
	}
	return &f, nil
}

// FilesDownload implements `files.download`, returning the shape
// internal/step.maybeMaterializeFile expects: base64 content plus mime, so
// a `file`-typed output port gets a real blob-store reference regardless of
// whether the op behind it is live or a fixture.
func (c *Client) FilesDownload(id string) (contentBase64, mimeType string, err error) {
	meta, err := c.FilesGet(id)
	if err != nil {
		return "", "", fmt.Errorf("files.download: %w", err)
	}
	token, err := c.Token()
	if err != nil {
		return "", "", fmt.Errorf("files.download: %w", err)
	}
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/files/%s?alt=media", driveBase, id), nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("files.download: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", "", fmt.Errorf("files.download failed (%d): %s", resp.StatusCode, truncate(data))
	}
	return base64.StdEncoding.EncodeToString(data), meta.MimeType, nil
}
