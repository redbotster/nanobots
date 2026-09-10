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
