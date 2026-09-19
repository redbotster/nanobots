package oneclaw

import (
	"errors"
	"fmt"
)

type memoryPutRequest struct {
	Value string `json:"value"`
}

type memoryEntry struct {
	Value string `json:"value"`
}

// MemoryGet reads one namespaced key from an agent's 1Claw-hosted memory. A
// 404 is not an error here — it means "not set yet", matching the
// step.Deps.MemoryGet contract (value, found, error).
func (c *Client) MemoryGet(agentID, namespace, key string) (string, bool, error) {
	var entry memoryEntry
	err := c.do("GET", fmt.Sprintf("/v1/agents/%s/memory/%s/%s", agentID, namespace, key), nil, &entry)
	if err != nil {
		var apiErr *apiError
		if errors.As(err, &apiErr) && apiErr.Status == 404 {
			return "", false, nil
		}
		return "", false, err
	}
	return entry.Value, true, nil
}

func (c *Client) MemoryPut(agentID, namespace, key, value string) error {
	path := fmt.Sprintf("/v1/agents/%s/memory/%s/%s", agentID, namespace, key)
	return c.do("PUT", path, memoryPutRequest{Value: value}, nil)
}

type memorySearchRequest struct {
	Namespace string `json:"namespace"`
	Query     string `json:"query"`
	TopK      int    `json:"top_k,omitempty"`
}

// MemorySearchResult is one match, verbatim from the API — see
// MemorySearch's doc comment for why Score is not treated as meaningful.
type MemorySearchResult struct {
	Key   string  `json:"key"`
	Value string  `json:"value"`
	Score float64 `json:"score"`
}

type memorySearchResponse struct {
	Results []MemorySearchResult `json:"results"`
}

// MemorySearch finds entries in one namespace of an agent's memory whose
// text relates to query, most relevant first, at most topK of them.
//
// Probed live against a real agent (docs/1claw-feature-requests.md #12):
// exact and partial substring queries score and rank correctly (1.0 for an
// exact match, lower for a partial one), but a paraphrase sharing no words
// with the stored text scores nothing — this is lexical matching, not
// semantic search, whatever ranking machinery sits behind the score. Callers
// should phrase queries the way the stored text was likely phrased, not
// assume synonyms are found.
func (c *Client) MemorySearch(agentID, namespace, query string, topK int) ([]MemorySearchResult, error) {
	path := fmt.Sprintf("/v1/agents/%s/memory/search", agentID)
	var out memorySearchResponse
	err := c.do("POST", path, memorySearchRequest{Namespace: namespace, Query: query, TopK: topK}, &out)
	if err != nil {
		return nil, err
	}
	return out.Results, nil
}
