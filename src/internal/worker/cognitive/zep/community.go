package zep

import (
	"fmt"
	"sort"
	"strings"
)

func (a *Algorithm) rebuildCommunities() {
	adj := map[string]map[string]struct{}{}
	for _, e := range a.graph.facts {
		if e.InvalidAt != "" {
			continue
		}
		if adj[e.SrcEntityID] == nil {
			adj[e.SrcEntityID] = map[string]struct{}{}
		}
		if adj[e.DstEntityID] == nil {
			adj[e.DstEntityID] = map[string]struct{}{}
		}
		adj[e.SrcEntityID][e.DstEntityID] = struct{}{}
		adj[e.DstEntityID][e.SrcEntityID] = struct{}{}
	}

	a.graph.communities = map[string]communityNode{}
	a.graph.communityByEntity = map[string]string{}
	visited := map[string]bool{}
	cidx := 0
	for entID := range a.graph.entities {
		if visited[entID] {
			continue
		}
		cidx++
		comp := a.collectComponent(entID, adj, visited)
		sort.Strings(comp)
		cid := fmt.Sprintf("com_%03d", cidx)
		names := make([]string, 0, len(comp))
		for _, id := range comp {
			names = append(names, a.graph.entities[id].Name)
			a.graph.communityByEntity[id] = cid
		}
		a.graph.communities[cid] = communityNode{
			ID:        cid,
			EntityIDs: comp,
			Summary:   "community: " + strings.Join(names, ", "),
		}
	}
}

func (a *Algorithm) collectComponent(seed string, adj map[string]map[string]struct{}, visited map[string]bool) []string {
	q := []string{seed}
	visited[seed] = true
	out := []string{seed}
	for len(q) > 0 {
		cur := q[0]
		q = q[1:]
		for nxt := range adj[cur] {
			if visited[nxt] {
				continue
			}
			visited[nxt] = true
			q = append(q, nxt)
			out = append(out, nxt)
		}
	}
	return out
}
