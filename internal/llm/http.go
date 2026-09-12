package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// defaultTimeout bounds one generation. Long enough for a slow model on a
// long prompt, short enough that a wedged provider fails the step instead
// of holding a container open until the run's own timeout.
const defaultTimeout = 5 * time.Minute

func newHTTPClient() *http.Client {
	return &http.Client{Timeout: defaultTimeout}
}

// maxAttempts bounds retries of a transient provider failure. Three is
// enough to ride out the common case and few enough that a genuinely down
// provider fails the step in seconds rather than minutes.
const maxAttempts = 3

// retryable reports whether a status is worth trying again. Rate limits and
// the 5xx family are the provider saying "not now"; a 400 or a 401 is it
// saying "not ever", and repeating those just wastes the run's clock.
func retryable(status int) bool {
	return status == http.StatusTooManyRequests || (status >= 500 && status <= 599)
}

// retryAfter reads the provider's own advice on when to come back. Google
// and Anthropic both send it, and it beats guessing — the alternative is
// backing off for two seconds when the quota window resets in twenty.
func retryAfter(h http.Header, attempt int) time.Duration {
	if v := h.Get("Retry-After"); v != "" {
		if secs, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && secs >= 0 {
			if d := time.Duration(secs) * time.Second; d <= 60*time.Second {
				return d
			}
			// Longer than a minute means the quota window, not a blip.
			// Waiting that out inside one step would hold a container open
			// for no good reason.
			return 0
		}
	}
	return time.Duration(1<<attempt) * time.Second // 1s, 2s
}

// postJSON is the shared request path for every direct backend: send JSON,
// insist on a 2xx, decode JSON — retrying a transient failure.
//
// Retrying is not gold-plating. The first real Gemini call this package
// ever made returned 503 "model is overloaded" and took down a whole swarm;
// the identical call succeeded seconds later. Providers 429 and 503 as a
// matter of course, and a run that has already done real work should not be
// lost to one of them.
//
// The error deliberately carries the provider's own message rather than
// just a status code. "429 from anthropic" sends someone to the wrong
// place; "429 ... your organization has exceeded" does not — and the memory
// work in this repo already showed how much a truncated upstream error
// costs when something fails at 2am.
func postJSON(ctx context.Context, c *http.Client, url string, headers map[string]string, in, out any) error {
	body, err := json.Marshal(in)
	if err != nil {
		return err
	}

	var lastErr error
	var lastWait time.Duration
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(lastWait):
			}
		}
		// A fresh reader each attempt: the previous one is drained.
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		for k, v := range headers {
			req.Header.Set(k, v)
		}

		resp, err := c.Do(req)
		if err != nil {
			// A transport failure (connection reset, DNS blip) is as
			// transient as a 503 and gets the same treatment.
			lastErr = fmt.Errorf("llm: %w", err)
			lastWait = time.Duration(1<<attempt) * time.Second
			continue
		}
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			if out == nil {
				return nil
			}
			if err := json.Unmarshal(raw, out); err != nil {
				return fmt.Errorf("llm: parse response from %s: %w", url, err)
			}
			return nil
		}

		lastErr = fmt.Errorf("llm: %s returned %d: %s", url, resp.StatusCode, truncate(raw))
		if !retryable(resp.StatusCode) {
			return lastErr
		}
		wait := retryAfter(resp.Header, attempt)
		if wait == 0 {
			// The provider asked for longer than this step should hold.
			return lastErr
		}
		lastWait = wait
	}
	return fmt.Errorf("%w (gave up after %d attempts)", lastErr, maxAttempts)
}

func truncate(b []byte) string {
	const max = 400
	if len(b) <= max {
		return string(b)
	}
	return string(b[:max]) + "…"
}
