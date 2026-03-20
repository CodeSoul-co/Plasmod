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

	subgraph := OneHopExpand(req.SeedObjectIDs[0], nodes, edges, req.EdgeTypes)

	return GraphExpandResponse{
		Subgraph:       subgraph,
		AppliedFilters: req.EdgeTypes,
	}
}