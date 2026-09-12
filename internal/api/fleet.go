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

// The Fleet answers a different question from the bot library. The library
// is the catalog: every bot that exists, so you can see what can be snapped
// into what. The Fleet is "who works for me, and how have I told them to
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

// FleetStore persists which bots have been tuned. A plain JSON file next to
// the other state this daemon owns — the same reasoning as run history:
// small, written rarely, and readable with `cat`.
type FleetStore struct {
	Path string

	mu sync.Mutex
}

func (s *FleetStore) load() (map[string]tunedRecord, error) {
	out := map[string]tunedRecord{}
	if s.Path == "" {
		return out, nil
	}
	raw, err := os.ReadFile(s.Path)
	if os.IsNotExist(err) {
		return out, nil
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

func (s *FleetStore) save(m map[string]tunedRecord) error {
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
func (s *FleetStore) RecordTuned(botID, shipped string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, _ := s.load()
	if _, already := m[botID]; already {
		return nil
	}
	m[botID] = tunedRecord{Shipped: shipped}
	return s.save(m)
}

// Forget drops a bot from the fleet, for when its instructions are put back.
func (s *FleetStore) Forget(botID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, _ := s.load()
	delete(m, botID)
	return s.save(m)
}

func (s *FleetStore) Shipped(botID string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, _ := s.load()
	r, ok := m[botID]
	return r.Shipped, ok
}

// FleetMember is one tuned bot and where it works.
type FleetMember struct {
	BotID string `json:"bot_id"`
	Name  string `json:"name"`
	// Instructions is what it does now; Shipped is what it came with, so
	// the UI can show the change and offer to put it back.
	Instructions string   `json:"instructions"`
	Shipped      string   `json:"shipped"`
	UsedIn       []string `json:"used_in"`
}

// FleetTeam is a swarm that reads like a team: more than one LLM bot, or a
// bot fanned out over a list. Reported so the Fleet can show tuned bots in
// the context they actually work in.
type FleetTeam struct {
	Swarm   string   `json:"swarm"`
	Path    string   `json:"path"`
	Members []string `json:"members"`
	// Tuned is the subset of Members the user has customised.
	Tuned []string `json:"tuned"`
}

type fleetResponse struct {
	Members []FleetMember `json:"members"`
	Teams   []FleetTeam   `json:"teams"`
}

func (s *Server) handleFleet(w http.ResponseWriter, r *http.Request) {
	if s.Fleet == nil {
		writeJSON(w, http.StatusOK, fleetResponse{Members: []FleetMember{}, Teams: []FleetTeam{}})
		return
	}
	tuned, loadErr := s.Fleet.load()

	// Which swarms use which bot, so a tuned bot can say where it works.
	usedIn, teams := s.swarmMembership()

	members := make([]FleetMember, 0, len(tuned))
	for botID, rec := range tuned {
		nb, err := schema.LoadNanobot(filepath.Join(s.BotsDir, botID, "nanobot.yaml"))
		if err != nil {
			continue // a bot that's since been deleted isn't a fleet member
		}
		members = append(members, FleetMember{
			BotID:        botID,
			Name:         nb.Metadata.Name,
			Instructions: instructionsDefaultOf(nb),
			Shipped:      rec.Shipped,
			UsedIn:       nonNil(usedIn[botID]),
		})
	}
	sort.Slice(members, func(i, j int) bool { return members[i].BotID < members[j].BotID })

	// Mark which team members are tuned, and keep only teams worth showing.
	kept := make([]FleetTeam, 0, len(teams))
	for _, t := range teams {
		for _, m := range t.Members {
			if _, ok := tuned[m]; ok {
				t.Tuned = append(t.Tuned, m)
			}
		}
		t.Tuned = nonNil(t.Tuned)
		kept = append(kept, t)
	}
	sort.Slice(kept, func(i, j int) bool { return kept[i].Swarm < kept[j].Swarm })

	if loadErr != nil {
		w.Header().Set("X-Fleet-Warning", loadErr.Error())
	}
	writeJSON(w, http.StatusOK, fleetResponse{Members: members, Teams: kept})
}

// swarmMembership maps bot id -> swarm names, and lists every swarm with
// the bots in it.
func (s *Server) swarmMembership() (map[string][]string, []FleetTeam) {
	usedIn := map[string][]string{}
	var teams []FleetTeam

	entries, err := os.ReadDir(s.swarmsDir())
	if err != nil {
		return usedIn, teams
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
		team := FleetTeam{Swarm: sw.Metadata.Name, Path: e.Name()}
		seen := map[string]bool{}
		for _, b := range sw.Spec.Bots {
			botID, _, _ := strings.Cut(b.Use, "@")
			if botID == "" || seen[botID] {
				continue
			}
			seen[botID] = true
			team.Members = append(team.Members, botID)
			usedIn[botID] = append(usedIn[botID], sw.Metadata.Name)
		}
		sort.Strings(team.Members)
		teams = append(teams, team)
	}
	for k := range usedIn {
		sort.Strings(usedIn[k])
	}
	return usedIn, teams
}

func instructionsDefaultOf(nb *schema.Nanobot) string {
	for _, p := range nb.Spec.Ports.Inputs {
		if p.Name == "instructions" {
			return p.Default
		}
	}
	return ""
}
