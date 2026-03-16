package segmentstore

type Plan struct {
	CandidatePartitions []*Partition
	Meta                []PartitionMeta
}

type Planner struct{}

func NewPlanner() *Planner {
	return &Planner{}
}

func (p *Planner) Build(req SearchRequest, partitions []*Partition) Plan {
	candidates := make([]*Partition, 0, len(partitions))
	meta := make([]PartitionMeta, 0, len(partitions))

	for _, partition := range partitions {
		partitionMeta := partition.Meta()
		if req.Namespace != "" && partitionMeta.Namespace != req.Namespace {
			continue
		}
		if req.MinEventUnixTS > 0 || req.MaxEventUnixTS > 0 {
			if partitionMeta.MaxTS > 0 && req.MinEventUnixTS > 0 && partitionMeta.MaxTS < req.MinEventUnixTS {
				continue
			}
			if partitionMeta.MinTS > 0 && req.MaxEventUnixTS > 0 && partitionMeta.MinTS > req.MaxEventUnixTS {
				continue
			}
		}
		if !req.IncludeGrowing && partitionMeta.State == PartitionStateGrowing {
			continue
		}
		candidates = append(candidates, partition)
		meta = append(meta, partitionMeta)
	}

	return Plan{CandidatePartitions: candidates, Meta: meta}
}
