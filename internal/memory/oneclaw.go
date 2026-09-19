package memory

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/redbotster/nanobots/internal/oneclaw"
)

// OneClaw is memory on a 1Claw agent: key/value always, and recall wherever
// the underlying search actually matches something.
//
// The recall half was dropped once and re-added, and both times for a
// measured reason rather than an assumption — see
// docs/1claw-feature-requests.md #12. First probe (account then): `POST
// /v1/agents/{id}/memory/search` answered every query with nothing except
// an empty one, so this deliberately did not implement Recaller — a Recall
// that always said "nothing known" would be worse than ErrNoRecall, which
// at least tells a bot the backend cannot do it. Re-probed after 1Claw
// shipped fixes:
//
//	PUT    .../memory/probe-ns2/search-probe  "refunds always get escalated to a human" -> stored
//	search {"query":"refunds"}                                -> 1 result, score 0.95
//	search {"query":"escalated to human"}                     -> 1 result, score 0.90
//	search {"query":"refunds always get escalated to a human"} -> 1 result, score 1.0
//	search {"query":"what happens with refund requests"}       -> 0 results (no shared words)
//
// So Recall here is real, but lexical: it finds text that shares words with
// the question, not text that means the same thing. Told to the bot as what
// it is — see Recall's doc comment — rather than dressed up as Honcho's
// synthesized-answer style, which this is not.
type OneClaw struct {
	Client  *oneclaw.Client
	AgentID string
}

func (o *OneClaw) Get(_ context.Context, namespace, key string) (string, bool, error) {
	if err := validKey(namespace, key); err != nil {
		return "", false, err
	}
	return o.Client.MemoryGet(o.AgentID, namespace, key)
}

func (o *OneClaw) Put(_ context.Context, namespace, key, value string) error {
	if err := validKey(namespace, key); err != nil {
		return err
	}
	return o.Client.MemoryPut(o.AgentID, namespace, key, value)
}

// rememberTopK bounds how many past observations Recall folds into one
// answer. Unmeasured against a real multi-observation namespace — chosen to
// match Honcho's own default so a bot switching backends sees a similarly
// sized answer either way.
const rememberTopK = 5

// Remember stores one observation under a generated key, since — unlike
// Put — a caller noting something down has no natural key of its own.
func (o *OneClaw) Remember(_ context.Context, namespace, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	key := "obs-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	if err := validKey(namespace, key); err != nil {
		return err
	}
	return o.Client.MemoryPut(o.AgentID, namespace, key, text)
}

// Recall finds the stored observations that share the most words with
// question and hands back their raw text, most relevant first — not a
// synthesized sentence the way Honcho's dialectic answer is. Joining
// matches verbatim is the honest version of what 1Claw's search actually
// does (see the package doc); paraphrasing them would claim a reasoning
// step that endpoint does not perform.
func (o *OneClaw) Recall(_ context.Context, namespace, question string) (string, error) {
	question = strings.TrimSpace(question)
	if question == "" {
		return "", fmt.Errorf("recall needs a question")
	}
	results, err := o.Client.MemorySearch(o.AgentID, namespace, question, rememberTopK)
	if err != nil {
		return "", err
	}
	if len(results) == 0 {
		return "", nil
	}
	lines := make([]string, len(results))
	for i, r := range results {
		lines[i] = r.Value
	}
	return strings.Join(lines, "\n"), nil
}

var (
	_ Store    = (*OneClaw)(nil)
	_ Recaller = (*OneClaw)(nil)
)
