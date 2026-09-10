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
		d.Edges[fromEp.BotID] = append(d.Edges[fromEp.BotID], toEp.BotID)
	}

	if _, err := d.TopoSort(); err != nil {
		return nil, err
	}
	return d, nil
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
