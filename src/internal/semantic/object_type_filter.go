package semantic

import (
	"strings"

	"plasmod/src/internal/schemas"
)

// defaultQueryableObjectTypes is used when the client omits object_types or
// sends an empty list: retrieval should consider memory, state, and artifact
// — never interpret "empty" as "match nothing".
var defaultQueryableObjectTypes = []string{
	string(schemas.ObjectTypeMemory),
	string(schemas.ObjectTypeState),
	string(schemas.ObjectTypeArtifact),
}

var knownQueryableTypes = map[string]struct{}{
	"memory":   {},
	"state":    {},
	"artifact": {},
}

// EffectiveObjectTypes returns the object-type filter to apply for a query.
// If the request omits object_types or sends an empty slice, it returns the
// full default set (memory, state, artifact). If the request lists only unknown
// values after normalization, it falls back to the same default so the query
// cannot accidentally return zero hits solely due to a bad filter.
func EffectiveObjectTypes(requested []string) []string {
	if len(requested) == 0 {
		return append([]string(nil), defaultQueryableObjectTypes...)
	}
	out := make([]string, 0, len(requested))
	seen := make(map[string]struct{})
	for _, t := range requested {
		k := strings.ToLower(strings.TrimSpace(t))
		if _, ok := knownQueryableTypes[k]; !ok {
			continue
		}
		if _, dup := seen[k]; dup {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, k)
	}
	if len(out) == 0 {
		return append([]string(nil), defaultQueryableObjectTypes...)
	}
	return out
}

// CanonicalObjectKindFromID infers the query object kind from a canonical object ID
// prefix (mem_, state_, art_). Returns empty string when the prefix is unknown.
func CanonicalObjectKindFromID(objectID string) string {
	switch {
	case strings.HasPrefix(objectID, schemas.IDPrefixMemory):
		return "memory"
	case strings.HasPrefix(objectID, schemas.IDPrefixState):
		return "state"
	case strings.HasPrefix(objectID, schemas.IDPrefixArtifact):
		return "artifact"
	default:
		return ""
	}
}

// FilterObjectIDsByTypes keeps object IDs whose inferred kind is allowed, or
// whose kind cannot be inferred (forward-compatible). When allowed is the full
// default triple, the input slice is returned without allocation.
func FilterObjectIDsByTypes(ids []string, allowed []string) []string {
	if len(ids) == 0 {
		return ids
	}
	allow := make(map[string]struct{}, len(allowed))
	for _, a := range allowed {
		allow[strings.ToLower(strings.TrimSpace(a))] = struct{}{}
	}
	if len(allow) >= 3 {
		_, m := allow["memory"]
		_, s := allow["state"]
		_, a := allow["artifact"]
		if m && s && a {
			return ids
		}
	}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		kind := CanonicalObjectKindFromID(id)
		if kind == "" {
			out = append(out, id)
			continue
		}
		if _, ok := allow[kind]; ok {
			out = append(out, id)
		}
	}
	return out
}
