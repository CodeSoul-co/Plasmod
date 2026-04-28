package zep

import (
	"math"
	"regexp"
	"sort"
	"strings"
	"time"
)

var nonWord = regexp.MustCompile(`[^a-zA-Z0-9_\-\x{4e00}-\x{9fff}]+`)

func tsNow() string { return time.Now().UTC().Format(time.RFC3339) }

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func normalizeToken(s string) string {
	return strings.ToLower(strings.TrimSpace(nonWord.ReplaceAllString(s, " ")))
}

func tokenSet(s string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, t := range strings.Fields(normalizeToken(s)) {
		if len(t) < 2 {
			continue
		}
		out[t] = struct{}{}
	}
	return out
}

func overlapScore(a, b map[string]struct{}) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	var inter int
	for k := range a {
		if _, ok := b[k]; ok {
			inter++
		}
	}
	den := math.Max(float64(len(a)), float64(len(b)))
	return clamp01(float64(inter) / den)
}

func freshnessFromValidFrom(validFrom, now string) float64 {
	if validFrom == "" || now == "" {
		return 0.5
	}
	t0, err0 := time.Parse(time.RFC3339, validFrom)
	t1, err1 := time.Parse(time.RFC3339, now)
	if err0 != nil || err1 != nil || t1.Before(t0) {
		return 0.5
	}
	hours := t1.Sub(t0).Hours()
	return clamp01(1.0 / (1.0 + hours/168.0))
}

func sortRank(rank rankedList) {
	sort.SliceStable(rank, func(i, j int) bool { return rank[i].Score > rank[j].Score })
}
