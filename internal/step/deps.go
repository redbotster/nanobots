package step

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/redbotster/nanobots/internal/llm"
	"github.com/redbotster/nanobots/internal/schema"
)

// FileValue is what a `file`-typed port actually carries on the context bus:
// a content-addressed reference, never the bytes themselves. See blueprint
// §3.4. The scheme is fixed as "nbf" (nanobots file) so it's unambiguous
// against a real URL if one ever ends up in the same field.
type FileValue struct {
	URI  string `json:"uri"`
	Mime string `json:"mime,omitempty"`
}

// BlobStore is the on-disk, content-addressed store behind `file` ports.
type BlobStore interface {
	Write(data []byte, mime string) (FileValue, error)
	Read(uri string) ([]byte, error)
}

// FSBlobStore is the default BlobStore: files live under dir/sha256/<hash>.
type FSBlobStore struct{ Dir string }

func NewFSBlobStore(dir string) (*FSBlobStore, error) {
	if err := os.MkdirAll(filepath.Join(dir, "sha256"), 0o700); err != nil {
		return nil, err
	}
	return &FSBlobStore{Dir: dir}, nil
}

func (s *FSBlobStore) Write(data []byte, mime string) (FileValue, error) {
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	path := filepath.Join(s.Dir, "sha256", hash)
	if _, err := os.Stat(path); err != nil {
		if err := os.WriteFile(path, data, 0o600); err != nil {
			return FileValue{}, fmt.Errorf("write blob: %w", err)
		}
	}
	return FileValue{URI: "nbf://sha256/" + hash, Mime: mime}, nil
}

// sha256Hex is what the digest part of every nbf:// reference must look
// like. Enforced here rather than only at the HTTP handler because it is a
// property of the URI format itself, so every caller gets the check — and
// because the one caller that forgot it turned this into an arbitrary file
// read (see handleGetBlob).
var sha256Hex = regexp.MustCompile(`^[0-9a-f]{64}$`)

func (s *FSBlobStore) Read(uri string) ([]byte, error) {
	const prefix = "nbf://sha256/"
	if len(uri) <= len(prefix) || uri[:len(prefix)] != prefix {
		return nil, fmt.Errorf("not an nbf:// blob reference: %q", uri)
	}
	digest := uri[len(prefix):]
	// Without this, "nbf://sha256/../../../../.secrets/nanobots.env" joins
	// to a real path outside the store and os.ReadFile happily returns it.
	if !sha256Hex.MatchString(digest) {
		return nil, fmt.Errorf("not a sha256 digest: %q", digest)
	}
	return os.ReadFile(filepath.Join(s.Dir, "sha256", digest))
}

// Deps is everything a step needs from the outside world. Interpret never
// talks to 1Claw, Docker, or the filesystem directly — every side effect
// goes through one of these methods, so the same interpreter runs identically
// against DemoDeps (fixtures, no network) and a future LiveDeps (real 1Claw
// Human API + Shroud + Browser Bridge).
type Deps interface {
	// ServiceCall executes one `service.call` step's op against the named
	// service, returning the op's result as a generic JSON-shaped value.
	ServiceCall(svc schema.Service, op string, params map[string]any) (any, error)
	// AIGenerate runs one `ai.generate` step's fully-resolved prompt through
	// whatever LLM backend this Deps uses, returning the raw text response.
	AIGenerate(prompt string, model schema.Model) (string, error)
	// Render turns a template file plus data into bytes in the given format
	// ("pdf" is the only format the blueprint's two example bots use).
	Render(templatePath string, data any, to string) ([]byte, string, error) // bytes, mime, error
	Now() string                                                             // RFC3339, injectable for tests
	MemoryGet(namespace, key string) (string, bool, error)
	MemoryPut(namespace, key, value string) error
	// MemoryRecall answers a question from what a bot has accumulated,
	// rather than looking up a key. Only a recall-capable backend can do
	// this (see internal/memory); the others return a clear error saying
	// so, which is why this is a distinct method rather than MemoryGet
	// with a special key.
	MemoryRecall(namespace, question string) (string, error)
	// MemoryRemember records one observation for a recall-capable backend
	// to derive from later. A no-op on key/value backends: noting
	// something down should not fail a run.
	MemoryRemember(namespace, text string) error
	// Approve blocks until a human decides. DemoDeps auto-approves so
	// conformance tests don't hang; the real runner surfaces this to the
	// WebUI/run log and actually waits.
	Approve(summary, riskTier string) (approved bool, decidedBy string, err error)
	Notify(message, channel string) error
	// WebFetch executes a `web.fetch` step (params.url or params.urls) —
	// credential-free, but still routed through Deps rather than called
	// directly, so DemoDeps can serve a fixture instead of a real network
	// call (conformance/tests must never depend on the internet).
	WebFetch(params map[string]any) (any, error)
	Blobs() BlobStore
	// GenerateWithTools runs one turn of an agent.loop step's tool-calling
	// conversation. Only a ToolCaller-capable backend can do this (see
	// internal/llm); DemoDeps replays a recorded transcript instead of
	// calling one, the same relationship AIGenerate has to a fixture.
	GenerateWithTools(messages []llm.Message, tools []llm.ToolDef, model schema.Model) (*llm.ToolCallResult, error)
}

// FallbackBlobStore writes to Primary and reads from Primary first, then
// Fallback.
//
// It exists because a bot container could not read a file another bot
// produced. A file output is moved into nanobotd's own blob store on the
// host when its container exits; the next bot's container gets a fresh,
// empty store on a tmpfs, and nothing mounted the host's. So a snap of
// `drafter.draft_html -> writer.voice_sample` type-checked, delivered a
// correct nbf:// reference in inputs.json, and then failed inside the
// container with "read file input: no such file or directory". File inputs
// only ever worked when passed straight to a service.call, because that
// round-trips to the host, which does have the blob.
//
// Reads fall back to a read-only mount of the host store; writes stay in
// the container's own scratch, so a bot still cannot modify a blob another
// run produced.
type FallbackBlobStore struct {
	Primary  BlobStore
	Fallback BlobStore
}

func (s *FallbackBlobStore) Write(data []byte, mime string) (FileValue, error) {
	return s.Primary.Write(data, mime)
}

func (s *FallbackBlobStore) Read(uri string) ([]byte, error) {
	data, err := s.Primary.Read(uri)
	if err == nil {
		return data, nil
	}
	if s.Fallback == nil {
		return nil, err
	}
	return s.Fallback.Read(uri)
}
