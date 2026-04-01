package schemas

func GetJoinKey(n GraphNode) (string, bool) {
	if n.Properties == nil {
		return "", false
	}
	v, ok := n.Properties["join_key"]
	if !ok || v == nil {
		return "", false
	}
	s, ok := v.(string)
	if !ok || s == "" {
		return "", false
	}
	return s, true
}

func FindNodeByJoinKey(nodes []GraphNode, joinKey string) (GraphNode, bool) {
	for _, n := range nodes {
		if jk, ok := GetJoinKey(n); ok && jk == joinKey {
			return n, true
		}
	}
	return GraphNode{}, false
}
