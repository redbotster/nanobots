package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/redbotster/nanobots/internal/schema"
)

// The Team answers a different question from the bot library. The library
// is the catalog: every bot that exists, so you can see what can be snapped
// into what. The Team is "who works for me, and how have I told them to
// behave" — only the bots whose instructions you've actually changed, and
// the swarms they work in.
//
// Knowing a bot has been tuned needs a record of what it shipped with,
// because editing overwrites the default in its nanobot.yaml. That record
// is also what makes "put it back" possible, which is the affordance that
// makes tuning safe to experiment with.

// tunedRecord remembers what a bot shipped with, written the first time
// each of its two independently-tunable things is changed and never
// overwritten after.
//
// The two are tracked independently, not as one "is this bot tuned"
// flag: a bot can have its guardrails tuned with its instructions never
// touched, or the other way round, and each needs its own "put back"
// target. Shipped is a pointer (not a bare string, as it used to be)
// for the same reason GuardrailsShipped is one — nil means "never
// recorded," which a bare "" cannot distinguish from "recorded, and it
// shipped with nothing set." Before this, a bot guardrails-tuned first
// created a record with no Shipped at all, and the existing "only the
// first call records" check on RecordTuned then saw a record already
// existed and skipped recording instructions' real shipped value the
// first time they were later touched — silently breaking "put back" for
// instructions on any bot tuned in that order.
type tunedRecord struct {
	Shipped           *string            `json:"shipped,omitempty"`
	GuardrailsShipped *schema.Guardrails `json:"guardrails_shipped,omitempty"`
}

// TeamStore persists which bots have been tuned. A plain JSON file next to
// the other state this daemon owns — the same reasoning as run history:
// small, written rarely, and readable with `cat`.
type TeamStore struct {
	Path string

	mu sync.Mutex
}

func (s *TeamStore) load() (map[string]tunedRecord, error) {
	out := map[string]tunedRecord{}
	if s.Path == "" {
		return out, nil
	}
	raw, err := os.ReadFile(s.Path)
	if os.IsNotExist(err) {
		// This file was called fleet.json before the tab was renamed to
		// Team. Read the old name once so a rename doesn't quietly throw
		// away every instruction someone had written — the whole point of
		// recording what a bot shipped with is that a change can be undone,
		// and losing the record loses that too.
		legacy, lerr := os.ReadFile(filepath.Join(filepath.Dir(s.Path), "fleet.json"))
		if lerr != nil {
			return out, nil
		}
		raw, err = legacy, nil
	}
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		// A corrupt file must not make every bot look untuned forever, but
		// it also must not be silently replaced — report and start clean.
		return map[string]tunedRecord{}, err
	}
	return out, nil
}

func (s *TeamStore) save(m map[string]tunedRecord) error {
	if s.Path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.Path, raw, 0o600)
}

// RecordTuned notes that botID's instructions have been customised,
// keeping whatever they shipped with. Only the first call records — the
// shipped value is the original, not the previous edit — and it checks
// specifically whether instructions were already recorded, not merely
// whether the bot has any record at all: a bot already present only for
// its guardrails must still get its instructions' shipped value recorded
// the first time those are touched too.
func (s *TeamStore) RecordTuned(botID, shipped string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, _ := s.load()
	rec := m[botID]
	if rec.Shipped != nil {
		return nil
	}
	rec.Shipped = &shipped
	m[botID] = rec
	return s.save(m)
}

// RecordGuardrailsTuned mirrors RecordTuned for guardrails.
func (s *TeamStore) RecordGuardrailsTuned(botID string, shipped schema.Guardrails) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, _ := s.load()
	rec := m[botID]
	if rec.GuardrailsShipped != nil {
		return nil
	}
	rec.GuardrailsShipped = &shipped
	m[botID] = rec
	return s.save(m)
}

// ForgetInstructions clears just the instructions half of a bot's record,
// for when they're put back to what shipped — dropping the bot from the
// team entirely only if its guardrails were never tuned either, so a
// guardrails customisation already on record survives.
func (s *TeamStore) ForgetInstructions(botID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, _ := s.load()
	rec, ok := m[botID]
	if !ok {
		return nil
	}
	rec.Shipped = nil
	if rec.GuardrailsShipped == nil {
		delete(m, botID)
	} else {
		m[botID] = rec
	}
	return s.save(m)
}

// ForgetGuardrails mirrors ForgetInstructions for guardrails.
func (s *TeamStore) ForgetGuardrails(botID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, _ := s.load()
	rec, ok := m[botID]
	if !ok {
		return nil
	}
	rec.GuardrailsShipped = nil
	if rec.Shipped == nil {
		delete(m, botID)
	} else {
		m[botID] = rec
	}
	return s.save(m)
}

func (s *TeamStore) Shipped(botID string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, _ := s.load()
	r, ok := m[botID]
	if !ok || r.Shipped == nil {
		return "", false
	}
	return *r.Shipped, true
}

// ShippedGuardrails mirrors Shipped for guardrails.
func (s *TeamStore) ShippedGuardrails(botID string) (schema.Guardrails, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, _ := s.load()
	r, ok := m[botID]
	if !ok || r.GuardrailsShipped == nil {
		return schema.Guardrails{}, false
	}
	return *r.GuardrailsShipped, true
}

// TeamMember is one tuned bot and where it works. A bot appears here for
// either or both of two independent reasons — instructions changed,
// guardrails changed — and each carries its own "shipped with" / "tuned"
// pair so the UI can offer "put back" for each separately.
type TeamMember struct {
	BotID string `json:"bot_id"`
	Name  string `json:"name"`
	// Instructions is what it does now; Shipped is what it came with, so
	// the UI can show the change and offer to put it back.
	Instructions      string `json:"instructions"`
	Shipped           string `json:"shipped"`
	InstructionsTuned bool   `json:"instructions_tuned"`
	// HasInstructions says whether this bot has an instructions port at
	// all — a bot present here only for a guardrails tune may have none,
	// and the UI needs a real fact to hide that editor rather than a
	// guess from Instructions happening to be empty.
	HasInstructions bool `json:"has_instructions"`
	// Guardrails is what applies now; ShippedGuardrails is what it came
	// with. ShippedGuardrails is meaningless when GuardrailsTuned is
	// false — it is the zero value, not "shipped with nothing set", since
	// no bot in this catalog actually ships with every guardrail unset.
	Guardrails        schema.Guardrails `json:"guardrails"`
	ShippedGuardrails schema.Guardrails `json:"shipped_guardrails"`
	GuardrailsTuned   bool              `json:"guardrails_tuned"`
	UsedIn            []string          `json:"used_in"`
}

type teamResponse struct {
	Members []TeamMember `json:"members"`
}

func (s *Server) handleTeam(w http.ResponseWriter, r *http.Request) {
	if s.Team == nil {
		writeJSON(w, http.StatusOK, teamResponse{Members: []TeamMember{}})
		return
	}
	tuned, loadErr := s.Team.load()

	// Which swarms use which bot, so a tuned bot can say where it works.
	usedIn := s.swarmMembership()

	members := make([]TeamMember, 0, len(tuned))
	for botID, rec := range tuned {
		nb, err := schema.LoadNanobot(filepath.Join(s.BotsDir, botID, "nanobot.yaml"))
		if err != nil {
			continue // a bot that's since been deleted isn't a team member
		}
		member := TeamMember{
			BotID:           botID,
			Name:            nb.Metadata.Name,
			Instructions:    instructionsDefaultOf(nb),
			HasInstructions: hasInstructionsPort(nb),
			Guardrails:      nb.Spec.Guardrails,
			GuardrailsTuned: rec.GuardrailsShipped != nil,
			UsedIn:          nonNil(usedIn[botID]),
		}
		if rec.Shipped != nil {
			member.InstructionsTuned = true
			member.Shipped = *rec.Shipped
		}
		if rec.GuardrailsShipped != nil {
			member.ShippedGuardrails = *rec.GuardrailsShipped
		}
		members = append(members, member)
	}
	sort.Slice(members, func(i, j int) bool { return members[i].BotID < members[j].BotID })

	if loadErr != nil {
		w.Header().Set("X-Team-Warning", loadErr.Error())
	}
	writeJSON(w, http.StatusOK, teamResponse{Members: members})
}

// swarmMembership maps a bot id to the swarms it appears in, so a tuned
// bot's card can say where the tuning actually takes effect.
//
// It used to also return every swarm and its members, for a third Team
// section listing "swarms containing a bot you've tuned" — which showed the
// same relationship as the cards, from the other direction, read-only, and
// duplicated what the Swarms page already does. Cut, and the API surface
// with it: a field nothing reads is a field someone later has to reason
// about.
func (s *Server) swarmMembership() map[string][]string {
	usedIn := map[string][]string{}
	_ = schema.ForEachSwarmFile(s.swarmsDir(), func(_ string, sw *schema.Nanoswarm) bool {
		seen := map[string]bool{}
		for _, b := range sw.Spec.Bots {
			botID, _, _ := strings.Cut(b.Use, "@")
			if botID == "" || seen[botID] {
				continue
			}
			seen[botID] = true
			usedIn[botID] = append(usedIn[botID], sw.Metadata.Name)
		}
		return true
	})
	for k := range usedIn {
		sort.Strings(usedIn[k])
	}
	return usedIn
}

func instructionsDefaultOf(nb *schema.Nanobot) string {
	for _, p := range nb.Spec.Ports.Inputs {
		if p.Name == "instructions" {
			return p.Default
		}
	}
	return ""
}
