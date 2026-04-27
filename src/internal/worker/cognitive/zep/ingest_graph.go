package zep

import (
	"fmt"
	"regexp"
	"strings"

	"plasmod/src/internal/schemas"
)

var capWord = regexp.MustCompile(`\b[A-Z][a-zA-Z0-9_\-]{1,}\b`)

func (a *Algorithm) ingestOneMemoryToGraph(m schemas.Memory, now string) {
	epID := "ep_" + m.MemoryID
	ep := episodeNode{
		ID:        epID,
		MemoryID:  m.MemoryID,
		AgentID:   m.AgentID,
		SessionID: m.SessionID,
		Content:   strings.TrimSpace(strings.TrimSpace(m.Content + " " + m.Summary)),
		TSValid:   chooseTS(m.ValidFrom, now),
		TSTx:      now,
	}
	a.graph.episodes[epID] = ep
	a.memoryByID[m.MemoryID] = m

	entityIDs := a.extractAndResolveEntities(ep)
	a.extractFactsAndInvalidate(ep, entityIDs)
	if a.cfg.Zep.Community.DynamicUpdate {
		a.rebuildCommunities()
	}
}

func chooseTS(ts, fallback string) string {
	if strings.TrimSpace(ts) != "" {
		return ts
	}
	return fallback
}

func (a *Algorithm) extractAndResolveEntities(ep episodeNode) []string {
	names := map[string]struct{}{}
	for _, m := range capWord.FindAllString(ep.Content, -1) {
		names[m] = struct{}{}
	}
	if ep.AgentID != "" {
		names[ep.AgentID] = struct{}{}
	}
	ids := make([]string, 0, len(names))
	for name := range names {
		key := normalizeToken(name)
		if key == "" {
			continue
		}
		id, ok := a.graph.entityByName[key]
		if !ok {
			id = fmt.Sprintf("ent_%s", strings.ReplaceAll(key, " ", "_"))
			a.graph.entities[id] = entityNode{
				ID:          id,
				Name:        name,
				Summary:     "entity from episode",
				EpisodeRefs: map[string]struct{}{ep.ID: {}},
			}
			a.graph.entityByName[key] = id
		} else {
			e := a.graph.entities[id]
			if e.EpisodeRefs == nil {
				e.EpisodeRefs = map[string]struct{}{}
			}
			e.EpisodeRefs[ep.ID] = struct{}{}
			a.graph.entities[id] = e
		}
		ids = append(ids, id)
	}
	return ids
}

func (a *Algorithm) extractFactsAndInvalidate(ep episodeNode, entityIDs []string) {
	if len(entityIDs) < 2 {
		return
	}
	validAt, invalidAt := a.extractTemporalRange(ep.Content, ep.TSValid)

	for i := 0; i < len(entityIDs); i++ {
		for j := i + 1; j < len(entityIDs); j++ {
			src, dst := entityIDs[i], entityIDs[j]
			predicate := inferPredicate(ep.Content)
			id := fmt.Sprintf("fact_%s_%s_%s_%s", src, dst, predicate, ep.ID)
			edge := factEdge{
				ID:           id,
				SrcEntityID:  src,
				DstEntityID:  dst,
				Predicate:    predicate,
				Fact:         fmt.Sprintf("%s(%s,%s)", predicate, src, dst),
				ValidAt:      validAt,
				InvalidAt:    invalidAt,
				CreatedAtTx:  ep.TSTx,
				EpisodeRefID: ep.ID,
			}
			a.invalidateContradictingEdges(src, dst, predicate, validAt)
			a.graph.facts[id] = edge
			a.graph.factsByEntity[src] = append(a.graph.factsByEntity[src], id)
			a.graph.factsByEntity[dst] = append(a.graph.factsByEntity[dst], id)
		}
	}
}

func inferPredicate(content string) string {
	lc := strings.ToLower(content)
	switch {
	case strings.Contains(lc, " not "), strings.Contains(lc, " no "):
		return "NEGATES"
	case strings.Contains(lc, " works "), strings.Contains(lc, "work at"):
		return "WORKS_WITH"
	case strings.Contains(lc, " likes "), strings.Contains(lc, "love"):
		return "PREFERS"
	default:
		return "RELATED_TO"
	}
}

func (a *Algorithm) invalidateContradictingEdges(src, dst, predicate, validAt string) {
	for _, fid := range a.graph.factsByEntity[src] {
		f := a.graph.facts[fid]
		if (f.SrcEntityID == src && f.DstEntityID == dst) || (f.SrcEntityID == dst && f.DstEntityID == src) {
			if contradiction(f.Predicate, predicate) && f.InvalidAt == "" {
				f.InvalidAt = chooseTS(validAt, tsNow())
				f.ExpiredAtTx = tsNow()
				a.graph.facts[fid] = f
			}
		}
	}
}

func contradiction(oldP, newP string) bool {
	if oldP == newP {
		return false
	}
	return (oldP == "NEGATES" && newP != "NEGATES") || (oldP != "NEGATES" && newP == "NEGATES")
}
