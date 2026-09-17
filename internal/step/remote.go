package step

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/redbotster/nanobots/internal/memory"
	"github.com/redbotster/nanobots/internal/schema"
)

// RemoteDeps is what runs *inside* a bot's container. It never holds a real
// 1Claw credential — every credentialed operation (service.call, ai.generate,
// memory, approve, notify) is a callback to nanobotd over CallbackURL,
// authenticated with a random per-run token that means nothing outside that
// one run. nanobotd is the only thing that ever holds the real 1Claw API key
// or an agent's ocv_ key (see internal/runner, which resolves each callback
// to a real LiveDeps or fixture-backed DemoDeps on the host side).
//
// Render/Now/Blobs stay local — rendering needs no credential, and there's
// no reason to round-trip the clock or blob storage through the host.
type RemoteDeps struct {
	CallbackURL string
	RunToken    string
	HTTPClient  *http.Client
	Blobstore   BlobStore
}

func NewRemoteDeps(callbackURL, runToken string, blobs BlobStore) *RemoteDeps {
	return &RemoteDeps{
		CallbackURL: callbackURL,
		RunToken:    runToken,
		// Must outlast the longest thing a callback can legitimately block
		// on, which is a human deciding an approval:
		// runner.ApprovalTimeout is 30 minutes, and bots/approve,
		// bots/email-send-approved and bots/email-drive-file all declare
		// max_runtime_secs: 1800 to match. This used to be 5 minutes with
		// the comment "approvals can wait a while", so a human who
		// approved at minute six got "context deadline exceeded" and a
		// failed run despite deciding well inside the declared window.
		HTTPClient: &http.Client{Timeout: 35 * time.Minute},
		Blobstore:  blobs,
	}
}

type remoteCallResponse struct {
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
}

func (r *RemoteDeps) call(path string, in any, out any) error {
	body, err := json.Marshal(in)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, r.CallbackURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+r.RunToken)
	resp, err := r.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("callback %s failed: %w", path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("callback %s: nanobotd returned %d: %s", path, resp.StatusCode, string(raw))
	}
	var rc remoteCallResponse
	if err := json.Unmarshal(raw, &rc); err != nil {
		return fmt.Errorf("callback %s: parse response: %w", path, err)
	}
	if rc.Error != "" {
		// Unprefixed, unlike every other error in this function.
		//
		// The others are about the callback itself — it did not connect, it
		// answered a non-200, its body did not parse — and naming the path
		// is the only way to say which one. This one is the daemon
		// succeeding at the callback and relaying a failure it has already
		// worded for a person: "no connected account yet ... connect it
		// from Settings". Prefixing that with `callback
		// /internal/steps/notify:` put the plumbing in front of the
		// sentence, on the Runs page, where nobody can act on the path.
		return errors.New(rc.Error)
	}
	if out != nil && len(rc.Result) > 0 {
		if err := json.Unmarshal(rc.Result, out); err != nil {
			return fmt.Errorf("callback %s: parse result: %w", path, err)
		}
	}
	return nil
}

func (r *RemoteDeps) ServiceCall(svc schema.Service, op string, params map[string]any) (any, error) {
	var out any
	err := r.call("/internal/steps/service_call", map[string]any{"service": svc, "op": op, "params": params}, &out)
	return out, err
}

func (r *RemoteDeps) AIGenerate(prompt string, model schema.Model) (string, error) {
	var out string
	err := r.call("/internal/steps/ai_generate", map[string]any{"prompt": prompt, "model": model}, &out)
	return out, err
}

func (r *RemoteDeps) MemoryGet(namespace, key string) (string, bool, error) {
	var out struct {
		Value string `json:"value"`
		Found bool   `json:"found"`
	}
	err := r.call("/internal/steps/memory_get", map[string]any{"namespace": namespace, "key": key}, &out)
	return out.Value, out.Found, err
}

func (r *RemoteDeps) MemoryRecall(namespace, question string) (string, error) {
	var out struct {
		Answer    string `json:"answer"`
		Supported *bool  `json:"supported"`
	}
	err := r.call("/internal/steps/memory_recall",
		map[string]any{"namespace": namespace, "question": question}, &out)
	if err != nil {
		return "", err
	}
	// Rebuild the sentinel the host flattened for the wire, so the
	// interpreter can tell "no recall here" from "recall failed".
	if out.Supported != nil && !*out.Supported {
		return "", memory.ErrNoRecall
	}
	return out.Answer, nil
}

func (r *RemoteDeps) MemoryRemember(namespace, text string) error {
	return r.call("/internal/steps/memory_remember",
		map[string]any{"namespace": namespace, "text": text}, nil)
}

func (r *RemoteDeps) MemoryPut(namespace, key, value string) error {
	return r.call("/internal/steps/memory_put", map[string]any{"namespace": namespace, "key": key, "value": value}, nil)
}

func (r *RemoteDeps) Approve(summary, riskTier string) (bool, string, error) {
	var out struct {
		Approved  bool   `json:"approved"`
		DecidedBy string `json:"decided_by"`
	}
	err := r.call("/internal/steps/approve", map[string]any{"summary": summary, "risk_tier": riskTier}, &out)
	return out.Approved, out.DecidedBy, err
}

func (r *RemoteDeps) Notify(message, channel string) error {
	return r.call("/internal/steps/notify", map[string]any{"message": message, "channel": channel}, nil)
}

func (r *RemoteDeps) Render(templatePath string, data any, to string) ([]byte, string, error) {
	switch to {
	case "pdf":
		return RenderHTMLToPDF(templatePath, data)
	case "png":
		return RenderHTMLToPNG(templatePath, data)
	}
	html, err := RenderHTML(templatePath, data)
	return html, "text/html", err
}

func (r *RemoteDeps) Now() string { return time.Now().UTC().Format(time.RFC3339) }

// WebFetch, unlike Render/Now, does round-trip through nanobotd — whether it
// serves a fixture (DemoDeps) or does a real fetch (LiveDeps) genuinely
// differs, the same reason ServiceCall/AIGenerate/Notify all proxy too.
func (r *RemoteDeps) WebFetch(params map[string]any) (any, error) {
	var out any
	err := r.call("/internal/steps/web_fetch", map[string]any{"params": params}, &out)
	return out, err
}

func (r *RemoteDeps) Blobs() BlobStore { return r.Blobstore }
