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

// tunedRecord remembers the suggestion a bot shipped with, written the
// first time its instructions are changed and never overwritten after.
type tunedRecord struct {
	Shipped string `json:"shipped"`
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

// RecordTuned notes that botID has been customised, keeping whatever it
// shipped with. Only the first call records — the shipped value is the
// original, not the previous edit.
func (s *TeamStore) RecordTuned(botID, shipped string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, _ := s.load()
	if _, already := m[botID]; already {
		return nil
	}
	m[botID] = tunedRecord{Shipped: shipped}
	return s.save(m)
}

// Forget drops a bot from the team, for when its instructions are put back.
func (s *TeamStore) Forget(botID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, _ := s.load()
	delete(m, botID)
	return s.save(m)
}

func (s *TeamStore) Shipped(botID string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, _ := s.load()
	r, ok := m[botID]
	return r.Shipped, ok
}

// TeamMember is one tuned bot and where it works.
type TeamMember struct {
	BotID string `json:"bot_id"`
	Name  string `json:"name"`
	// Instructions is what it does now; Shipped is what it came with, so
	// the UI can show the change and offer to put it back.
	Instructions string   `json:"instructions"`
	Shipped      string   `json:"shipped"`
	UsedIn       []string `json:"used_in"`
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
		members = append(members, TeamMember{
			BotID:        botID,
			Name:         nb.Metadata.Name,
			Instructions: instructionsDefaultOf(nb),
			Shipped:      rec.Shipped,
			UsedIn:       nonNil(usedIn[botID]),
		})
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
	entries, err := os.ReadDir(s.swarmsDir())
	if err != nil {
		return usedIn
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		path := filepath.Join(s.swarmsDir(), e.Name())
		sw, err := schema.LoadNanoswarm(path)
		if err != nil {
			continue
		}
		seen := map[string]bool{}
		for _, b := range sw.Spec.Bots {
			botID, _, _ := strings.Cut(b.Use, "@")
			if botID == "" || seen[botID] {
				continue
			}
			seen[botID] = true
			usedIn[botID] = append(usedIn[botID], sw.Metadata.Name)
		}
	}
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
