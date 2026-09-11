package step

import (
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/redbotster/nanobots/internal/google"
	"github.com/redbotster/nanobots/internal/oneclaw"
)

// GoogleConfig is what LiveDeps needs to reach Gmail/Drive/Sheets directly,
// once a Google OAuth "Desktop app" client id exists (GOOGLE_OAUTH_CLIENT_ID
// — see internal/google's package doc) and a bot's Google service is
// switched from `connection: demo` to `connection: oauth_native`. The
// refresh token is deliberately one shared secret per vault, not one per
// bot instance — every bot that needs Gmail/Drive uses the same connected
// Google account (see docs/connections.md).
type GoogleConfig struct {
	ClientID        string
	VaultID         string
	RefreshTokenKey string // defaults to "google/refresh_token"
}

func (cfg GoogleConfig) refreshTokenKey() string {
	if cfg.RefreshTokenKey == "" {
		return "google/refresh_token"
	}
	return cfg.RefreshTokenKey
}

func (cfg GoogleConfig) configured() bool {
	return cfg.ClientID != "" && cfg.VaultID != ""
}

// googleTokenSource turns a 1Claw-vaulted refresh token into the
// google.TokenSource callback google.Client needs, minting a fresh access
// token only once the cached one is within a minute of expiring. The
// refresh token itself is fetched from the vault at most once per process —
// Google refresh tokens don't rotate on use.
type googleTokenSource struct {
	oc  *oneclaw.Client
	cfg GoogleConfig

	mu           sync.Mutex
	refreshToken string
	accessToken  string
	expiry       time.Time
}

func (g *googleTokenSource) Token() (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.accessToken != "" && time.Now().Before(g.expiry.Add(-1*time.Minute)) {
		return g.accessToken, nil
	}
	if g.refreshToken == "" {
		rt, err := g.oc.GetSecret(g.cfg.VaultID, g.cfg.refreshTokenKey())
		if err != nil {
			if locked, ok := oneclaw.AsVaultLocked(err); ok {
				return "", locked
			}
			return "", fmt.Errorf("google: no connected account yet (%w) — run `nanobots connect google` first", err)
		}
		g.refreshToken = rt
	}
	tr, err := google.RefreshAccessToken(g.cfg.ClientID, g.refreshToken)
	if err != nil {
		return "", fmt.Errorf("google: refresh access token: %w", err)
	}
	g.accessToken = tr.AccessToken
	g.expiry = time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second)
	return g.accessToken, nil
}

// googleAPI is the subset of *google.Client's methods dispatchGoogle calls —
// a narrow interface, not *google.Client directly, so the op-dispatch logic
// below is unit-testable with a fake instead of a real HTTP server. Mirrors
// this package's existing BlobStore/Approver pattern.
type googleAPI interface {
	MessagesList(q string, max int) ([]google.Message, error)
	MessagesSend(to, subject, body string) (string, error)
	MessagesModify(urgentIDs, laterIDs []string) error
	DraftsCreate(to, subject, body string) (string, error)
	DraftsSend(draftID string) (string, error)
	FilesGet(id string) (*google.DriveFile, error)
	FilesList(folder string, max int) ([]google.DriveFile, error)
	FilesCreate(folder, filename string, data []byte, mimeType string) (*google.DriveFile, error)
	FilesDownload(id string) (contentBase64, mimeType string, err error)
	RowsAppend(sheetID string, values []any) (rowNumber string, err error)
	EventsList(calendarID, timeMin, timeMax string) ([]google.Event, error)
}

// googleClient lazily builds the real *google.Client, reusing one
// googleTokenSource (and therefore one cached access token) across every
// call for the lifetime of this LiveDeps.
func (l *LiveDeps) googleClient() (googleAPI, error) {
	if !l.Google.configured() {
		return nil, fmt.Errorf("google: not configured (need GOOGLE_OAUTH_CLIENT_ID and a vault) — see docs/connections.md")
	}
	if l.googleTS == nil {
		l.googleTS = &googleTokenSource{oc: l.OneClaw, cfg: l.Google}
	}
	return google.NewClient(l.googleTS.Token), nil
}

// dispatchGoogle executes one service.call op against a real Google account,
// shaping each result exactly like the matching bots/*/fixtures/*.json fixture
// so a bot's downstream steps and prompts never need to know whether they're
// on demo or live data (see bots/*/nanobot.yaml `op:` values for the full op
// list this must cover).
func dispatchGoogle(c googleAPI, op string, params map[string]any, blobs BlobStore) (any, error) {
	switch op {
	case "messages.list":
		q, _ := params["q"].(string)
		msgs, err := c.MessagesList(q, paramInt(params["max"], 200))
		if err != nil {
			return nil, err
		}
		return toJSONAny(msgs)

	case "messages.send":
		to, _ := params["to"].(string)
		subject, _ := params["subject"].(string)
		body, _ := params["body"].(string)
		return c.MessagesSend(to, subject, body)

	case "messages.modify":
		urgentIDs := paramIDs(params["urgent"])
		laterIDs := paramIDs(params["later"])
		if err := c.MessagesModify(urgentIDs, laterIDs); err != nil {
			return nil, err
		}
		return map[string]any{"modified": len(urgentIDs) + len(laterIDs)}, nil

	case "drafts.create":
		items, _ := params["drafts"].([]any)
		ids := make([]string, 0, len(items))
		for _, it := range items {
			d, _ := it.(map[string]any)
			to, _ := d["to"].(string)
			subject, _ := d["subject"].(string)
			body, _ := d["body"].(string)
			id, err := c.DraftsCreate(to, subject, body)
			if err != nil {
				return nil, fmt.Errorf("drafts.create: %w", err)
			}
			ids = append(ids, id)
		}
		return map[string]any{"draft_ids": ids}, nil

	case "drafts.send":
		draftID, _ := params["draft_id"].(string)
		return c.DraftsSend(draftID)

	case "files.get":
		id, _ := params["id"].(string)
		f, err := c.FilesGet(id)
		if err != nil {
			return nil, err
		}
		return toJSONAny(f)

	case "files.list":
		folder, _ := params["folder"].(string)
		files, err := c.FilesList(folder, paramInt(params["max"], 1))
		if err != nil {
			return nil, err
		}
		if len(files) == 0 {
			return nil, fmt.Errorf("files.list: no files found in %q", folder)
		}
		// Matches the demo fixture shape: "the most recent file", a single
		// object, not an array — see bots/drive-watch/fixtures.
		return toJSONAny(files[0])

	case "files.create":
		folder, _ := params["folder"].(string)
		data, mime, err := resolveFileParam(params["file"], blobs)
		if err != nil {
			return nil, fmt.Errorf("files.create: %w", err)
		}
		// nanobot.yaml's files.create op has no `filename` param (see
		// bots/recap-emails-to-pdf, bots/drive-save) — this synthesizes one
		// from the current time and mime type. A real op contract would add
		// an explicit filename input; tracked as a gap, not hidden.
		f, err := c.FilesCreate(folder, syntheticFilename(mime), data, mime)
		if err != nil {
			return nil, err
		}
		return toJSONAny(f)

	case "files.download":
		id, _ := params["id"].(string)
		b64, mime, err := c.FilesDownload(id)
		if err != nil {
			return nil, err
		}
		return map[string]any{"content_base64": b64, "mime": mime}, nil

	case "rows.append":
		sheet, _ := params["sheet"].(string)
		values, _ := params["values"].(map[string]any)
		rowNumber, err := c.RowsAppend(sheet, flattenRowValues(values))
		if err != nil {
			return nil, err
		}
		return map[string]any{"row_number": rowNumber, "values": values}, nil

	case "events.list":
		calendarID, _ := params["calendar_id"].(string)
		timeMin, _ := params["time_min"].(string)
		timeMax, _ := params["time_max"].(string)
		events, err := c.EventsList(calendarID, timeMin, timeMax)
		if err != nil {
			return nil, err
		}
		return toJSONAny(events)

	default:
		return nil, fmt.Errorf("google: unsupported op %q", op)
	}
}

// toJSONAny converts a typed google.* struct (or slice of them) into the
// same map[string]any/[]any shape a fixture's raw JSON unmarshals into —
// required because lookupPath (template.go) only walks map[string]any and
// []any, not arbitrary Go structs via reflection. Without this, a live
// result would silently fail every downstream `{{steps.x.output.field}}`
// lookup instead of erroring.
func toJSONAny(v any) (any, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("shape live result as JSON: %w", err)
	}
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("shape live result as JSON: %w", err)
	}
	return out, nil
}

// paramInt reads a JSON-decoded numeric param (always float64 once it's
// round-tripped through encoding/json) as an int, falling back to def.
func paramInt(v any, def int) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	default:
		return def
	}
}

// paramIDs pulls the "id" field out of each element of a []any of
// map[string]any — the shape triage's `urgent`/`later` list<json> outputs
// take (see bots/inbox-triage/prompts/triage.md).
func paramIDs(v any) []string {
	items, _ := v.([]any)
	ids := make([]string, 0, len(items))
	for _, it := range items {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		if id, ok := m["id"].(string); ok {
			ids = append(ids, id)
		}
	}
	return ids
}

// flattenRowValues turns an arbitrary JSON object payload into one ordered
// row (sorted by key, for determinism) — Sheets has no notion of "the field
// named X goes in column Y" without a declared header mapping, which this
// build's schema has nowhere to express yet. A bot relying on column order
// needs its sheet's columns to match this alphabetical order. A documented
// gap, matching RowsAppend's own sheet-id-not-name note.
func flattenRowValues(values map[string]any) []any {
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	row := make([]any, 0, len(keys))
	for _, k := range keys {
		row = append(row, values[k])
	}
	return row
}

// resolveFileParam reads the bytes and mime type behind a `file`-typed
// param, which arrives on the context bus as a FileValue (in-process) or the
// JSON-decoded equivalent map[string]any{"uri":..., "mime":...} (after a
// round trip through /run/inputs.json).
func resolveFileParam(v any, blobs BlobStore) (data []byte, mime string, err error) {
	var uri string
	switch fv := v.(type) {
	case FileValue:
		uri, mime = fv.URI, fv.Mime
	case map[string]any:
		uri, _ = fv["uri"].(string)
		mime, _ = fv["mime"].(string)
	default:
		return nil, "", fmt.Errorf("expected a file-typed value, got %T", v)
	}
	if uri == "" {
		return nil, "", fmt.Errorf("file value has no uri")
	}
	data, err = blobs.Read(uri)
	if err != nil {
		return nil, "", err
	}
	if mime == "" {
		mime = "application/octet-stream"
	}
	return data, mime, nil
}

var mimeExtensions = map[string]string{
	"application/pdf":  ".pdf",
	"text/markdown":    ".md",
	"text/plain":       ".txt",
	"text/html":        ".html",
	"application/json": ".json",
}

func syntheticFilename(mime string) string {
	return "nanobots-" + time.Now().UTC().Format("20060102-150405") + mimeExtensions[mime]
}
