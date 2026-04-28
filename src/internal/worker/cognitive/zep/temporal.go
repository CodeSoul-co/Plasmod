package zep

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	reYearsAgo = regexp.MustCompile(`(\d+)\s+years?\s+ago`)
	reDaysAgo  = regexp.MustCompile(`(\d+)\s+days?\s+ago`)
)

func (a *Algorithm) extractTemporalRange(content, refTS string) (string, string) {
	if refTS == "" {
		refTS = tsNow()
	}
	ref, err := time.Parse(time.RFC3339, refTS)
	if err != nil {
		return refTS, ""
	}

	lc := strings.ToLower(content)
	if m := reYearsAgo.FindStringSubmatch(lc); len(m) == 2 {
		n, _ := strconv.Atoi(m[1])
		v := ref.AddDate(-n, 0, 0).UTC().Format(time.RFC3339)
		return v, ""
	}
	if m := reDaysAgo.FindStringSubmatch(lc); len(m) == 2 {
		n, _ := strconv.Atoi(m[1])
		v := ref.AddDate(0, 0, -n).UTC().Format(time.RFC3339)
		return v, ""
	}
	if a.cfg.Zep.TemporalExtraction.PresentTenseUsesReferenceTime {
		return refTS, ""
	}
	return "", ""
}
