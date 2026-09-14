package oneclaw

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// 1Claw's OpenTelemetry surface: what is set up, how it is doing, and what
// is happening right now.
//
// Everything else in this package asks 1Claw to *do* something — provision
// an agent, read a secret, open an approval. This is the only part that
// asks what it already knows, and it knows a lot that nanobots otherwise
// cannot see. Every bot this repo runs gets its own 1Claw agent holding a
// policy that grants a vault path, and until now that graph existed only in
// 1Claw's own dashboard. Twenty-four of the twenty-five agents on the
// account this was built against are nanobots-provisioned, and nothing in
// this app showed one of them.

// TopologyNode is one thing in the org graph: an agent, the policy it
// holds, or the vault that policy grants.
type TopologyNode struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"`  // agent | vault | policy | connector | chain
	Label string `json:"label"` // the agent name, the vault name, the policy's path glob
	// Status and Trust are only meaningful for an agent; a vault and a
	// policy come back with neither, which is why both are omitempty
	// rather than defaulted to something that would read as a real value.
	Status string `json:"status,omitempty"`
	Trust  *int   `json:"trust,omitempty"`
}

// TopologyEdge is a directed relationship: an agent `holds` a policy, a
// policy `grants` a vault.
type TopologyEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Kind string `json:"kind"`
}

type Topology struct {
	Nodes []TopologyNode `json:"nodes"`
	Edges []TopologyEdge `json:"edges"`
	// Truncated is 1Claw telling us the graph is bigger than what came
	// back. Passed through rather than hidden: a topology view that
	// silently omits half an org is worse than one that says it did.
	Truncated  bool `json:"truncated"`
	TotalNodes int  `json:"total_nodes"`
}

// Posture is the headline numbers behind the org's security posture.
type Posture struct {
	Score            int `json:"posture_score"`
	OpenThreats      int `json:"open_threats"`
	OpenCritical     int `json:"open_critical"`
	PendingApprovals int `json:"pending_approvals"`
	AgentCount       int `json:"agent_count"`
	TopThreats       []struct {
		ID       string `json:"id"`
		Kind     string `json:"kind"`
		Severity string `json:"severity"`
		Summary  string `json:"summary"`
	} `json:"top_threats"`
}

// OTelTopology returns the agent/policy/vault graph for the org.
func (c *Client) OTelTopology() (*Topology, error) {
	var t Topology
	if err := c.do("GET", "/v1/otel/topology", nil, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

// OTelPosture returns the posture score and the counts behind it.
func (c *Client) OTelPosture() (*Posture, error) {
	var p Posture
	if err := c.do("GET", "/v1/otel/summary", nil, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

// ErrStreamRateLimited is 1Claw refusing another stream connection for now.
// A caller must back off rather than reconnect, which is why this is a
// sentinel and not just another status code in a message.
var ErrStreamRateLimited = errors.New("otel stream: rate limited by 1Claw")

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// Signal is one telemetry event off the live stream. A span, mostly:
// "oneclaw.policy.evaluate" with an outcome and the vault path it was
// deciding about.
type Signal struct {
	EventID   int64          `json:"event_id"`
	Kind      string         `json:"kind"`
	Name      string         `json:"name"`
	TraceID   string         `json:"trace_id,omitempty"`
	DurationM int            `json:"duration_ms,omitempty"`
	Outcome   string         `json:"outcome,omitempty"`
	Attrs     map[string]any `json:"attributes,omitempty"`
}

// StreamOTel follows the org's telemetry as it happens, calling onSignal for
// each one until ctx is cancelled or the connection drops.
//
// lastEventID resumes where a previous call stopped — the stream's events
// are numbered and it honours Last-Event-ID, so a reconnect does not have
// to replay from the beginning or skip what it missed.
//
// Returns the error that ended it, which is nearly always the caller's own
// cancellation. A caller that wants to stay connected reconnects; this
// deliberately does not retry on its own, so the decision about how hard to
// hammer 1Claw belongs to the layer that knows whether anyone is watching.
func (c *Client) StreamOTel(ctx context.Context, lastEventID string, onSignal func(Signal)) error {
	// The same short-lived token every other call uses, not the raw API
	// key: ensureToken exchanges it at /v1/auth/api-key-token and refreshes
	// before expiry, and a stream that outlives the token would otherwise
	// be the one caller quietly doing its own thing.
	token, err := c.ensureToken()
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		strings.TrimRight(c.BaseURL, "/")+"/v1/otel/stream", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "text/event-stream")
	if lastEventID != "" {
		req.Header.Set("Last-Event-ID", lastEventID)
	}

	// Not c.HTTPClient: that one carries a 30-second timeout, which is
	// right for a request/response call and fatal for a stream — it would
	// cut "real time" off every thirty seconds, and the reconnect would
	// look like flaky telemetry rather than a client bug. The context is
	// the only thing that ends this.
	resp, err := streamHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("otel stream: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		// Named, because a caller that reconnects on every error will sit
		// here forever making it worse. Found by probing this endpoint a
		// handful of times in a row while developing: it rate-limits, and
		// the first symptom was a connection that appeared to hang.
		return fmt.Errorf("%w: retry-after %s", ErrStreamRateLimited,
			firstNonEmpty(resp.Header.Get("Retry-After"), "unspecified"))
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("otel stream: 1Claw answered %d", resp.StatusCode)
	}

	sc := bufio.NewScanner(resp.Body)
	// A span carrying a big attribute bag can exceed bufio's 64KB default,
	// and a single oversized line would otherwise end the whole stream.
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		data, ok := strings.CutPrefix(line, "data: ")
		if !ok {
			continue // id:, event:, comments, and the blank line between frames
		}
		var sig Signal
		if err := json.Unmarshal([]byte(data), &sig); err != nil {
			continue // one malformed frame is not a reason to stop watching
		}
		onSignal(sig)
	}
	return sc.Err()
}

// streamHTTPClient has no overall timeout on purpose — see StreamOTel. It
// keeps a response-header timeout so a connection that never answers at all
// still fails fast rather than hanging forever.
//
// Cloned from http.DefaultTransport rather than built fresh: a bare
// &http.Transport{} silently drops proxy-from-environment, the TLS defaults
// and HTTP/2, and the first version of this hung for the whole context
// instead of connecting.
var streamHTTPClient = &http.Client{Transport: streamTransport()}

func streamTransport() *http.Transport {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.ResponseHeaderTimeout = 30 * time.Second
	return t
}

// Quota is how much of the account's plan is used. The one that matters
// here is agents: this repo gives every distinct bot name its own 1Claw
// agent, so a busy catalog eats the allowance, and running out shows up as
// a 403 "Agent limit reached" from EnsureAgent at the exact moment a swarm
// tries to run a bot whose agent does not exist yet
// (docs/oneclaw-bridge.md). A number you can see beats an error you cannot
// predict.
type Quota struct {
	Tier  string `json:"tier"`
	Usage struct {
		Agents  Allowance `json:"agents"`
		Secrets Allowance `json:"secrets"`
		Vaults  Allowance `json:"vaults"`
	} `json:"usage"`
}

type Allowance struct {
	Used  int `json:"used"`
	Limit int `json:"limit"`
}

// Near reports whether this allowance is close enough to its limit to be
// worth warning about. Eighty percent: far enough out that there is time to
// delete an agent, close enough that it is not crying wolf.
func (a Allowance) Near() bool {
	return a.Limit > 0 && a.Used*100 >= a.Limit*80
}

// Quota returns the account's plan usage.
func (c *Client) Quota() (*Quota, error) {
	var q Quota
	if err := c.do("GET", "/v1/billing/subscription", nil, &q); err != nil {
		return nil, err
	}
	return &q, nil
}
