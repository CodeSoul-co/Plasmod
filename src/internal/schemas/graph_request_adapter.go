package schemas

func ResolveSeedObjectIDsFromJoinKeys(nodes []GraphNode, joinKeys []string) []string {
	if len(joinKeys) == 0 {
		return nil
	}

	out := make([]string, 0, len(joinKeys))
	seen := make(map[string]bool)

	for _, jk := range joinKeys {
		node, ok := FindNodeByJoinKey(nodes, jk)
		if !ok {
			continue
		}
		if !seen[node.ObjectID] {
			seen[node.ObjectID] = true
			out = append(out, node.ObjectID)
		}
	}

	return out
}

func ExpandFromJoinKeys(
	joinKeys []string,
	req GraphExpandRequest,
	nodes []GraphNode,
	edges []Edge,
) GraphExpandResponse {
	resolved := ResolveSeedObjectIDsFromJoinKeys(nodes, joinKeys)

	req.SeedObjectIDs = resolved
	return ExpandFromRequest(req, nodes, edges)
}
