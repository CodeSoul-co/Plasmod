package zep

import (
	"math"
	"strings"

	"plasmod/src/internal/schemas"
)

func (a *Algorithm) runSearch(query string, candidates []schemas.Memory, now string) map[string]rankedList {
	out := map[string]rankedList{}
	byID := map[string]schemas.Memory{}
	for _, c := range candidates {
		byID[c.MemoryID] = c
	}
	qtoks := tokenSet(query)
	if hasMethod(a.cfg.Zep.Retrieval.SearchMethods, "cosine") {
		out["cosine"] = a.searchCosine(qtoks, byID, now)
	}
	if hasMethod(a.cfg.Zep.Retrieval.SearchMethods, "bm25") {
		out["bm25"] = a.searchBM25(qtoks, byID, now)
	}
	if hasMethod(a.cfg.Zep.Retrieval.SearchMethods, "bfs") && a.cfg.Zep.Retrieval.BFS.Enabled {
		out["bfs"] = a.searchBFS(qtoks, byID, now)
	}
	return out
}

func (a *Algorithm) searchCosine(qtoks map[string]struct{}, byID map[string]schemas.Memory, now string) rankedList {
	out := rankedList{}
	for _, m := range byID {
		score := overlapScore(qtoks, tokenSet(m.Content+" "+m.Summary))
		score = 0.8*score + 0.2*freshnessFromValidFrom(m.ValidFrom, now)
		out = append(out, candidateScore{MemoryID: m.MemoryID, Score: clamp01(score), Signal: "cosine"})
	}
	sortRank(out)
	return out
}

func (a *Algorithm) searchBM25(qtoks map[string]struct{}, byID map[string]schemas.Memory, now string) rankedList {
	type tfDoc struct {
		id string
		tf map[string]int
		l  int
	}
	docs := make([]tfDoc, 0, len(byID))
	df := map[string]int{}
	for _, m := range byID {
		tf := map[string]int{}
		for _, t := range strings.Fields(normalizeToken(m.Content + " " + m.Summary)) {
			tf[t]++
		}
		seen := map[string]bool{}
		for t := range tf {
			if !seen[t] {
				df[t]++
				seen[t] = true
			}
		}
		docs = append(docs, tfDoc{id: m.MemoryID, tf: tf, l: len(tf)})
	}
	N := float64(len(docs))
	if N == 0 {
		return nil
	}
	var avgdl float64
	for _, d := range docs {
		avgdl += float64(maxInt(1, d.l))
	}
	avgdl /= N
	k1, b := 1.5, 0.75
	out := rankedList{}
	for _, d := range docs {
		var score float64
		for t := range qtoks {
			tf := float64(d.tf[t])
			if tf == 0 {
				continue
			}
			idf := math.Log(1 + (N-float64(df[t])+0.5)/(float64(df[t])+0.5))
			den := tf + k1*(1-b+b*float64(maxInt(1, d.l))/avgdl)
			score += idf * ((tf * (k1 + 1)) / den)
		}
		out = append(out, candidateScore{MemoryID: d.id, Score: clamp01(score), Signal: "bm25"})
	}
	sortRank(out)
	return out
}

func (a *Algorithm) searchBFS(qtoks map[string]struct{}, byID map[string]schemas.Memory, now string) rankedList {
	seedEntities := map[string]struct{}{}
	for _, e := range a.graph.entities {
		if overlapScore(qtoks, tokenSet(e.Name+" "+e.Summary)) > 0 {
			seedEntities[e.ID] = struct{}{}
		}
	}
	depth := map[string]int{}
	q := []string{}
	for id := range seedEntities {
		depth[id] = 0
		q = append(q, id)
	}
	for len(q) > 0 {
		cur := q[0]
		q = q[1:]
		if depth[cur] >= 2 {
			continue
		}
		for _, fid := range a.graph.factsByEntity[cur] {
			f := a.graph.facts[fid]
			if f.InvalidAt != "" {
				continue
			}
			for _, nxt := range []string{f.SrcEntityID, f.DstEntityID} {
				if _, ok := depth[nxt]; ok {
					continue
				}
				depth[nxt] = depth[cur] + 1
				q = append(q, nxt)
			}
		}
	}
	memScore := map[string]float64{}
	for entID, d := range depth {
		penalty := 1.0 / float64(d+1)
		for epID := range a.graph.entities[entID].EpisodeRefs {
			ep := a.graph.episodes[epID]
			if _, ok := byID[ep.MemoryID]; ok {
				if penalty > memScore[ep.MemoryID] {
					memScore[ep.MemoryID] = penalty
				}
			}
		}
	}
	out := rankedList{}
	for id, s := range memScore {
		out = append(out, candidateScore{MemoryID: id, Score: clamp01(s), Signal: "bfs"})
	}
	sortRank(out)
	return out
}

func (a *Algorithm) runRerank(searchRanks map[string]rankedList, byID map[string]schemas.Memory) rankedList {
	rank := rankedList{}
	if hasMethod(a.cfg.Zep.Retrieval.RerankMethods, "rrf") {
		rank = applyRRF(searchRanks, 60)
	} else {
		for _, lst := range searchRanks {
			rank = append(rank, lst...)
		}
		sortRank(rank)
	}
	if hasMethod(a.cfg.Zep.Retrieval.RerankMethods, "episode_mentions") {
		rank = a.applyEpisodeMentions(rank)
	}
	if hasMethod(a.cfg.Zep.Retrieval.RerankMethods, "node_distance") {
		rank = a.applyNodeDistance(rank)
	}
	if hasMethod(a.cfg.Zep.Retrieval.RerankMethods, "mmr") {
		rank = applyMMR(rank, byID, 0.7, a.cfg.Zep.Retrieval.TopK)
	}
	return rank
}

func applyRRF(ranks map[string]rankedList, k int) rankedList {
	if k <= 0 {
		k = 60
	}
	score := map[string]float64{}
	for _, lst := range ranks {
		for i, c := range lst {
			score[c.MemoryID] += 1.0 / float64(k+i+1)
		}
	}
	out := make(rankedList, 0, len(score))
	for id, s := range score {
		out = append(out, candidateScore{MemoryID: id, Score: s, Signal: "rrf"})
	}
	sortRank(out)
	return out
}

func applyMMR(rank rankedList, byID map[string]schemas.Memory, lambda float64, topK int) rankedList {
	if len(rank) == 0 {
		return rank
	}
	if topK <= 0 || topK > len(rank) {
		topK = len(rank)
	}
	selected := rankedList{}
	remaining := append(rankedList{}, rank...)
	for len(selected) < topK && len(remaining) > 0 {
		bestI := 0
		best := -1.0
		for i, c := range remaining {
			rel := c.Score
			div := 0.0
			for _, s := range selected {
				div = math.Max(div, memSimilarity(byID[c.MemoryID], byID[s.MemoryID]))
			}
			mmr := lambda*rel - (1-lambda)*div
			if mmr > best {
				best = mmr
				bestI = i
			}
		}
		selected = append(selected, remaining[bestI])
		remaining = append(remaining[:bestI], remaining[bestI+1:]...)
	}
	return selected
}

func memSimilarity(a, b schemas.Memory) float64 {
	return overlapScore(tokenSet(a.Content+" "+a.Summary), tokenSet(b.Content+" "+b.Summary))
}

func (a *Algorithm) applyEpisodeMentions(rank rankedList) rankedList {
	boost := map[string]float64{}
	for _, ep := range a.graph.episodes {
		boost[ep.MemoryID] += 0.01
	}
	out := append(rankedList{}, rank...)
	for i := range out {
		out[i].Score += boost[out[i].MemoryID]
		out[i].Signal = out[i].Signal + "+episode_mentions"
	}
	sortRank(out)
	return out
}

func (a *Algorithm) applyNodeDistance(rank rankedList) rankedList {
	if len(rank) == 0 {
		return rank
	}
	centroid := rank[0].MemoryID
	out := append(rankedList{}, rank...)
	for i := range out {
		d := a.memoryDistance(centroid, out[i].MemoryID)
		out[i].Score += 1.0 / float64(d+1)
		out[i].Signal = out[i].Signal + "+node_distance"
	}
	sortRank(out)
	return out
}

func (a *Algorithm) memoryDistance(aID, bID string) int {
	if aID == bID {
		return 0
	}
	aSet := map[string]struct{}{}
	bSet := map[string]struct{}{}
	for _, ep := range a.graph.episodes {
		if ep.MemoryID == aID {
			for _, e := range a.graph.entities {
				if _, ok := e.EpisodeRefs[ep.ID]; ok {
					aSet[e.ID] = struct{}{}
				}
			}
		}
		if ep.MemoryID == bID {
			for _, e := range a.graph.entities {
				if _, ok := e.EpisodeRefs[ep.ID]; ok {
					bSet[e.ID] = struct{}{}
				}
			}
		}
	}
	minD := 3
	for aEnt := range aSet {
		for bEnt := range bSet {
			if aEnt == bEnt {
				return 1
			}
			if a.graph.communityByEntity[aEnt] != "" && a.graph.communityByEntity[aEnt] == a.graph.communityByEntity[bEnt] {
				if minD > 2 {
					minD = 2
				}
			}
		}
	}
	return minD
}

func hasMethod(methods []string, target string) bool {
	for _, m := range methods {
		if strings.EqualFold(strings.TrimSpace(m), target) {
			return true
		}
	}
	return false
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
