package semantic

import (
	"time"

	"andb/src/internal/schemas"
)

type QueryPlan struct {
	TopK           int
	Namespace      string
	Constraints    []string
	TimeFromUnixTS int64
	TimeToUnixTS   int64
	IncludeGrowing bool
}

type QueryPlanner interface {
	Build(req schemas.QueryRequest) QueryPlan
}

type DefaultQueryPlanner struct{}

func NewDefaultQueryPlanner() *DefaultQueryPlanner {
	return &DefaultQueryPlanner{}
}

func (p *DefaultQueryPlanner) Build(req schemas.QueryRequest) QueryPlan {
	topK := req.TopK
	if topK <= 0 {
		topK = 10
	}
	ns := req.QueryScope
	if ns == "" {
		ns = req.SessionID
	}
	fromTS, _ := parseRFC3339ToUnix(req.TimeWindow.From)
	toTS, _ := parseRFC3339ToUnix(req.TimeWindow.To)
	return QueryPlan{
		TopK:           topK,
		Namespace:      ns,
		Constraints:    req.RelationConstraints,
		TimeFromUnixTS: fromTS,
		TimeToUnixTS:   toTS,
		IncludeGrowing: true,
	}
}

func parseRFC3339ToUnix(ts string) (int64, bool) {
	if ts == "" {
		return 0, false
	}
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return 0, false
	}
	return t.Unix(), true
}
