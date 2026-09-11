package step

import (
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

var webFetchClient = &http.Client{Timeout: 10 * time.Second}

const webFetchMaxBytes = 1 << 20 // 1MB cap — this is a summary input, not a mirror

var htmlStripRe = regexp.MustCompile(`(?is)<script.*?</script>|<style.*?</style>|<[^>]+>`)

// FetchedPage is one URL's web.fetch result.
type FetchedPage struct {
	URL  string `json:"url"`
	Text string `json:"text"`
}

// fetchURL performs a plain, credential-free GET and strips HTML tags to
// leave roughly-readable text — good enough as ai.generate input for a
// summary/ideas prompt (bots/content-ideas, bots/competitor-watch), not a
// real readability/boilerplate-removal extractor. Nothing here needs a
// vault secret or 1Claw at all, so a web.fetch step behaves identically
// under DemoDeps and LiveDeps — see runWebFetch in interpret.go.
func fetchURL(rawURL string) (FetchedPage, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return FetchedPage{}, fmt.Errorf("web.fetch %s: %w", rawURL, err)
	}
	req.Header.Set("User-Agent", "nanobots-web-fetch/1.0 (+https://github.com/redbotster/nanobots)")
	resp, err := webFetchClient.Do(req)
	if err != nil {
		return FetchedPage{}, fmt.Errorf("web.fetch %s: %w", rawURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return FetchedPage{}, fmt.Errorf("web.fetch %s: unexpected status %d", rawURL, resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, webFetchMaxBytes))
	if err != nil {
		return FetchedPage{}, fmt.Errorf("web.fetch %s: %w", rawURL, err)
	}
	text := htmlStripRe.ReplaceAllString(string(raw), " ")
	text = strings.Join(strings.Fields(text), " ")
	return FetchedPage{URL: rawURL, Text: text}, nil
}

// runWebFetch implements the `web.fetch` step type: params.url fetches one
// page (returns a single object), params.urls fetches each in turn (returns
// a list) — a bot declares whichever shape its own output port expects.
// guardrails.network_egress is a per-bot allowlist a human can read, but
// (like everywhere else in this build) isn't enforced as an actual network
// policy here — a pre-existing, documented gap, not new to this step.
func runWebFetch(params map[string]any) (any, error) {
	if urls, ok := params["urls"].([]any); ok {
		pages := make([]FetchedPage, 0, len(urls))
		for _, u := range urls {
			urlStr, _ := u.(string)
			page, err := fetchURL(urlStr)
			if err != nil {
				return nil, err
			}
			pages = append(pages, page)
		}
		return toJSONAny(pages)
	}
	url, _ := params["url"].(string)
	if url == "" {
		return nil, fmt.Errorf("web.fetch: params.url or params.urls is required")
	}
	page, err := fetchURL(url)
	if err != nil {
		return nil, err
	}
	return toJSONAny(page)
}
