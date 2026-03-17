package segmentstore

// Plan is the output of the Planner: the set of Shards that a search request
// should be executed against, along with their metadata snapshots.
type Plan struct {
	CandidateShards []*Shard
	ShardMetas      []ShardMeta
}

type Planner struct{}

func NewPlanner() *Planner {
	return &Planner{}
}

func (p *Planner) Build(req SearchRequest, shards []*Shard) Plan {
	candidates := make([]*Shard, 0, len(shards))
	metas := make([]ShardMeta, 0, len(shards))

	for _, shard := range shards {
		meta := shard.Meta()
		if req.Namespace != "" && meta.Namespace != req.Namespace {
			continue
		}
		if req.MinEventUnixTS > 0 || req.MaxEventUnixTS > 0 {
			if meta.MaxTS > 0 && req.MinEventUnixTS > 0 && meta.MaxTS < req.MinEventUnixTS {
				continue
			}
			if meta.MinTS > 0 && req.MaxEventUnixTS > 0 && meta.MinTS > req.MaxEventUnixTS {
				continue
			}
		}
		if !req.IncludeGrowing && meta.State == ShardStateGrowing {
			continue
		}
		candidates = append(candidates, shard)
		metas = append(metas, meta)
	}

	return Plan{CandidateShards: candidates, ShardMetas: metas}
}
