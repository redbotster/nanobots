package step

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

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
		HTTPClient:  &http.Client{Timeout: 5 * time.Minute}, // approvals can wait a while
		Blobstore:   blobs,
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
		return fmt.Errorf("callback %s: %s", path, rc.Error)
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
	if to == "pdf" {
		return RenderHTMLToPDF(templatePath, data)
	}
	html, err := RenderHTML(templatePath, data)
	return html, "text/html", err
}

func (r *RemoteDeps) Now() string { return time.Now().UTC().Format(time.RFC3339) }

func (r *RemoteDeps) Blobs() BlobStore { return r.Blobstore }
