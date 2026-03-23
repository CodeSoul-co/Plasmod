package schemas

func containsEdgeType(edgeType string, allowed []string) bool {
	if len(allowed) == 0 {
		return true
	}
	for _, t := range allowed {
		if t == edgeType {
			return true
		}
	}
	return false
}

func OneHopExpand(seedID string, nodes []GraphNode, edges []Edge, edgeTypes []string) EvidenceSubgraph {
	nodeIndex := make(map[string]GraphNode)
	for _, n := range nodes {
		nodeIndex[n.ObjectID] = n
	}

	nodeMap := make(map[string]GraphNode)
	resultEdges := make([]Edge, 0)

	for _, e := range edges {
		if (e.SrcObjectID == seedID || e.DstObjectID == seedID) && containsEdgeType(e.EdgeType, edgeTypes) {
			resultEdges = append(resultEdges, e)

			if srcNode, ok := nodeIndex[e.SrcObjectID]; ok {
				nodeMap[srcNode.ObjectID] = srcNode
			}
			if dstNode, ok := nodeIndex[e.DstObjectID]; ok {
				nodeMap[dstNode.ObjectID] = dstNode
			}
		}
	}

	resultNodes := make([]GraphNode, 0, len(nodeMap))
	for _, n := range nodeMap {
		resultNodes = append(resultNodes, n)
	}

	return EvidenceSubgraph{
		SeedIDs: []string{seedID},
		Nodes:   resultNodes,
		Edges:   resultEdges,
		ProofTrace: []ProofStep{
			{Step: 1, Operation: "seed_lookup", Detail: "load seed node"},
			{Step: 2, Operation: "edge_filter", Detail: "filter edges by edge types"},
			{Step: 3, Operation: "one_hop_expand", Detail: "collect directly connected edges and nodes"},
			{Step: 4, Operation: "subgraph_assemble", Detail: "assemble evidence subgraph"},
		},
	}
}

func ExpandFromRequest(req GraphExpandRequest, nodes []GraphNode, edges []Edge) GraphExpandResponse {
	if len(req.SeedObjectIDs) == 0 {
		return GraphExpandResponse{
			Subgraph: EvidenceSubgraph{},
		}
	}

	// Expand each seed and merge the results, deduplicating nodes and edges.
	nodeMap := make(map[string]GraphNode)
	edgeMap := make(map[string]Edge)
	var allSeeds []string

	for _, seedID := range req.SeedObjectIDs {
		sub := OneHopExpand(seedID, nodes, edges, req.EdgeTypes)
		allSeeds = append(allSeeds, sub.SeedIDs...)
		for _, n := range sub.Nodes {
			nodeMap[n.ObjectID] = n
		}
		for _, e := range sub.Edges {
			edgeMap[e.EdgeID] = e
		}
	}

	mergedNodes := make([]GraphNode, 0, len(nodeMap))
	for _, n := range nodeMap {
		mergedNodes = append(mergedNodes, n)
	}
	mergedEdges := make([]Edge, 0, len(edgeMap))
	for _, e := range edgeMap {
		mergedEdges = append(mergedEdges, e)
	}

	subgraph := EvidenceSubgraph{
		SeedIDs: allSeeds,
		Nodes:   mergedNodes,
		Edges:   mergedEdges,
		ProofTrace: []ProofStep{
			{Step: 1, Operation: "seed_lookup", Detail: "load seed nodes"},
			{Step: 2, Operation: "edge_filter", Detail: "filter edges by edge types"},
			{Step: 3, Operation: "multi_seed_expand", Detail: "collect directly connected edges and nodes for all seeds"},
			{Step: 4, Operation: "subgraph_assemble", Detail: "merge and deduplicate evidence subgraph"},
		},
	}

	return GraphExpandResponse{
		Subgraph:       subgraph,
		AppliedFilters: req.EdgeTypes,
	}
}
