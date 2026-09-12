package planner

import (
	"fmt"
	"sort"
)

// DAG is the bot-level run graph: an edge A -> B means a snap feeds A's
// output into B's input, so A must run (and complete) before B starts.
type DAG struct {
	Nodes []string            // bot ids, in a stable order
	Edges map[string][]string // from bot id -> to bot ids
}

// BuildDAG builds the run graph from a resolved swarm's snaps and detects
// cycles via Kahn's algorithm. Bots with no snaps at all are still included
// as isolated nodes (they can run immediately, in any order).
func BuildDAG(rs *ResolvedSwarm) (*DAG, error) {
	d := &DAG{Edges: map[string][]string{}}
	for id := range rs.Bots {
		d.Nodes = append(d.Nodes, id)
	}
	sort.Strings(d.Nodes)

	for _, snap := range rs.Swarm.Spec.Snaps {
		fromEp, err := ParseEndpoint(snap.From)
		if err != nil {
			return nil, err
		}
		toEp, err := ParseEndpoint(snap.To)
		if err != nil {
			return nil, err
		}
		if fromEp.BotID == toEp.BotID {
			return nil, fmt.Errorf("snap %s -> %s: a bot cannot snap into itself", snap.From, snap.To)
		}
		// Two snaps between the same pair of bots (e.g. a draft's id and its
		// subject both flowing from "replies" to "sender") must not become
		// two edges — indegree would count that dependency twice and the
		// printed DAG would show the target bot twice for one real
		// dependency.
		if !containsString(d.Edges[fromEp.BotID], toEp.BotID) {
			d.Edges[fromEp.BotID] = append(d.Edges[fromEp.BotID], toEp.BotID)
		}
	}

	if _, err := d.TopoSort(); err != nil {
		return nil, err
	}
	return d, nil
}

func containsString(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// TopoSort returns a valid run order, or an error naming a cycle.
func (d *DAG) TopoSort() ([]string, error) {
	indegree := map[string]int{}
	for _, n := range d.Nodes {
		indegree[n] = 0
	}
	for _, tos := range d.Edges {
		for _, to := range tos {
			indegree[to]++
		}
	}

	var queue []string
	for _, n := range d.Nodes {
		if indegree[n] == 0 {
			queue = append(queue, n)
		}
	}
	sort.Strings(queue)

	var order []string
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		order = append(order, n)

		var next []string
		for _, to := range d.Edges[n] {
			indegree[to]--
			if indegree[to] == 0 {
				next = append(next, to)
			}
		}
		sort.Strings(next)
		queue = append(queue, next...)
	}

	if len(order) != len(d.Nodes) {
		var stuck []string
		for _, n := range d.Nodes {
			if indegree[n] > 0 {
				stuck = append(stuck, n)
			}
		}
		sort.Strings(stuck)
		return nil, fmt.Errorf("cycle detected involving: %v", stuck)
	}
	return order, nil
}

// Levels groups the DAG into waves: every bot in a wave has all its
// dependencies satisfied by earlier waves, so a wave's bots can run at the
// same time.
//
// TopoSort flattens that structure away. It returns one valid order, and
// the runner walked it one bot at a time — so morning-brief, which is four
// bots in two waves of two, ran as four sequential container starts and
// four sequential rounds of model calls when half of that work never
// depended on the other half.
//
// Same algorithm as TopoSort (Kahn's), keeping the wave boundaries instead
// of discarding them, and sorted within each wave so a plan printed twice
// reads the same twice.
func (d *DAG) Levels() ([][]string, error) {
	indegree := map[string]int{}
	for _, n := range d.Nodes {
		indegree[n] = 0
	}
	for _, tos := range d.Edges {
		for _, to := range tos {
			indegree[to]++
		}
	}

	var ready []string
	for _, n := range d.Nodes {
		if indegree[n] == 0 {
			ready = append(ready, n)
		}
	}
	sort.Strings(ready)

	var levels [][]string
	placed := 0
	for len(ready) > 0 {
		wave := ready
		levels = append(levels, wave)
		placed += len(wave)

		var next []string
		for _, n := range wave {
			for _, to := range d.Edges[n] {
				indegree[to]--
				if indegree[to] == 0 {
					next = append(next, to)
				}
			}
		}
		sort.Strings(next)
		ready = next
	}

	if placed != len(d.Nodes) {
		// Same condition TopoSort reports, phrased the same way, so a
		// cycle reads identically whichever entry point found it.
		var stuck []string
		for _, n := range d.Nodes {
			if indegree[n] > 0 {
				stuck = append(stuck, n)
			}
		}
		sort.Strings(stuck)
		return nil, fmt.Errorf("cycle detected involving: %v", stuck)
	}
	return levels, nil
}

// Print renders the DAG as a text tree, in run order, for `nanobots plan`.
func (d *DAG) Print() string {
	order, err := d.TopoSort()
	if err != nil {
		return err.Error()
	}
	out := ""
	for _, n := range order {
		out += n
		if tos := d.Edges[n]; len(tos) > 0 {
			out += " -> " + fmt.Sprint(tos)
		}
		out += "\n"
	}
	return out
}
