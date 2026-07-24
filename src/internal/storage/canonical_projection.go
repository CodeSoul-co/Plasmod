package storage

import (
	"encoding/json"
	"fmt"

	"github.com/dgraph-io/badger/v4"

	"plasmod/src/internal/schemas"
)

func (c *compositeRuntimeStorage) ApplyCanonicalProjection(projection CanonicalProjection) error {
	obj, objOK := c.obj.(*badgerObjectStore)
	edges, edgesOK := c.edg.(*badgerGraphEdgeStore)
	versions, versionsOK := c.ver.(*badgerSnapshotVersionStore)
	if objOK && edgesOK && versionsOK && obj.db == edges.db && obj.db == versions.db {
		return applyBadgerCanonicalProjection(obj.db, projection)
	}
	_, memoryObjects := c.obj.(*memoryObjectStore)
	_, memoryEdges := c.edg.(*memoryGraphEdgeStore)
	_, memoryVersions := c.ver.(*memorySnapshotVersionStore)
	if memoryObjects && memoryEdges && memoryVersions {
		applyCanonicalProjection(c.obj, c.edg, c.ver, projection)
		return nil
	}
	return fmt.Errorf("canonical projection requires object, edge, and version stores to share one backend")
}

func (s *MemoryRuntimeStorage) ApplyCanonicalProjection(projection CanonicalProjection) error {
	applyCanonicalProjection(s.objectStore, s.edgeStore, s.versionStore, projection)
	return nil
}

func applyCanonicalProjection(
	objects ObjectStore,
	edges GraphEdgeStore,
	versions SnapshotVersionStore,
	projection CanonicalProjection,
) {
	if projection.Event != nil {
		objects.PutEvent(*projection.Event)
	}
	if projection.Memory != nil {
		objects.PutMemory(*projection.Memory)
	}
	if projection.State != nil {
		objects.PutState(*projection.State)
	}
	if projection.Artifact != nil {
		objects.PutArtifact(*projection.Artifact)
	}
	for _, version := range projection.Versions {
		versions.PutVersion(version)
	}
	for _, edge := range canonicalProjectionEdges(projection) {
		edges.PutEdge(edge)
	}
}

func applyBadgerCanonicalProjection(db *badger.DB, projection CanonicalProjection) error {
	return db.Update(func(txn *badger.Txn) error {
		if projection.Event != nil {
			if err := setJSONCountedTxn(txn, []byte(kpObjEvent+projection.Event.Identity.EventID), kpObjEvent, *projection.Event); err != nil {
				return fmt.Errorf("persist event %q: %w", projection.Event.Identity.EventID, err)
			}
		}
		if projection.Memory != nil {
			if err := setJSONCountedTxn(txn, []byte(kpObjMemory+projection.Memory.MemoryID), kpObjMemory, *projection.Memory); err != nil {
				return fmt.Errorf("persist memory %q: %w", projection.Memory.MemoryID, err)
			}
		}
		if projection.State != nil {
			if err := setJSONCountedTxn(txn, []byte(kpObjState+projection.State.StateID), kpObjState, *projection.State); err != nil {
				return fmt.Errorf("persist state %q: %w", projection.State.StateID, err)
			}
		}
		if projection.Artifact != nil {
			if err := setJSONCountedTxn(txn, []byte(kpObjArtifact+projection.Artifact.ArtifactID), kpObjArtifact, *projection.Artifact); err != nil {
				return fmt.Errorf("persist artifact %q: %w", projection.Artifact.ArtifactID, err)
			}
		}
		for _, version := range projection.Versions {
			inserted, err := putVersionTxn(txn, version)
			if err != nil {
				return fmt.Errorf("persist version for %q: %w", version.ObjectID, err)
			}
			if inserted {
				if err := badgerAddCounterTxn(txn, kpVer, 1); err != nil {
					return fmt.Errorf("count version for %q: %w", version.ObjectID, err)
				}
			}
		}
		for _, edge := range canonicalProjectionEdges(projection) {
			edgeKey := []byte(kpEdge + edge.EdgeID)
			exists, err := badgerTxnKeyExists(txn, edgeKey)
			if err != nil {
				return fmt.Errorf("check edge %q: %w", edge.EdgeID, err)
			}
			if err := putEdgeTxn(txn, edge); err != nil {
				return fmt.Errorf("persist edge %q: %w", edge.EdgeID, err)
			}
			if !exists {
				if err := badgerAddCounterTxn(txn, kpEdge, 1); err != nil {
					return fmt.Errorf("count edge %q: %w", edge.EdgeID, err)
				}
			}
		}
		return nil
	})
}

func canonicalProjectionEdges(projection CanonicalProjection) []schemas.Edge {
	total := len(projection.Edges)
	if projection.Event != nil && projection.IncludeEventBaseEdges {
		total += len(schemas.BuildEventBaseEdges(*projection.Event))
	}
	if projection.Memory != nil && projection.IncludeMemoryBaseEdges {
		total += len(schemas.BuildMemoryBaseEdges(*projection.Memory))
	}
	if projection.Artifact != nil && projection.IncludeArtifactBaseEdges {
		total += len(schemas.BuildArtifactBaseEdges(*projection.Artifact))
	}
	out := make([]schemas.Edge, 0, total)
	seenIDs := make(map[string]int, total)
	baseIDs := make(map[string]struct{}, 6)
	appendEdge := func(edge schemas.Edge, base bool) {
		if index, exists := seenIDs[edge.EdgeID]; exists {
			if _, isBase := baseIDs[edge.EdgeID]; isBase {
				out[index] = mergeCanonicalBaseEdge(out[index], edge)
			} else {
				out[index] = edge
			}
			return
		}
		seenIDs[edge.EdgeID] = len(out)
		if base {
			baseIDs[edge.EdgeID] = struct{}{}
		}
		out = append(out, edge)
	}
	if projection.Event != nil && projection.IncludeEventBaseEdges {
		for _, edge := range schemas.BuildEventBaseEdges(*projection.Event) {
			appendEdge(edge, true)
		}
	}
	if projection.Memory != nil && projection.IncludeMemoryBaseEdges {
		for _, edge := range schemas.BuildMemoryBaseEdges(*projection.Memory) {
			appendEdge(edge, true)
		}
	}
	if projection.Artifact != nil && projection.IncludeArtifactBaseEdges {
		for _, edge := range schemas.BuildArtifactBaseEdges(*projection.Artifact) {
			appendEdge(edge, true)
		}
	}
	for _, edge := range projection.Edges {
		appendEdge(edge, false)
	}
	return out
}

func mergeCanonicalBaseEdge(base, materialized schemas.Edge) schemas.Edge {
	if materialized.SrcType != "" {
		base.SrcType = materialized.SrcType
	}
	if materialized.DstType != "" {
		base.DstType = materialized.DstType
	}
	if materialized.Weight != 0 {
		base.Weight = materialized.Weight
	}
	if materialized.ProvenanceRef != "" {
		base.ProvenanceRef = materialized.ProvenanceRef
	}
	if materialized.CreatedTS != "" {
		base.CreatedTS = materialized.CreatedTS
	}
	if materialized.ExpiresAt != "" {
		base.ExpiresAt = materialized.ExpiresAt
	}
	if len(materialized.Properties) > 0 {
		if base.Properties == nil {
			base.Properties = make(map[string]any, len(materialized.Properties))
		}
		for key, value := range materialized.Properties {
			base.Properties[key] = value
		}
	}
	return base
}

func setJSONTxn(txn *badger.Txn, key []byte, value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return txn.Set(key, encoded)
}

func setJSONCountedTxn(txn *badger.Txn, key []byte, counterName string, value any) error {
	exists, err := badgerTxnKeyExists(txn, key)
	if err != nil {
		return err
	}
	if err := setJSONTxn(txn, key, value); err != nil {
		return err
	}
	if !exists {
		return badgerAddCounterTxn(txn, counterName, 1)
	}
	return nil
}

func putEdgeTxn(txn *badger.Txn, edge schemas.Edge) error {
	edgeKey := []byte(kpEdge + edge.EdgeID)
	if item, err := txn.Get(edgeKey); err == nil {
		var previous schemas.Edge
		if err := item.Value(func(value []byte) error { return json.Unmarshal(value, &previous) }); err != nil {
			return err
		}
		if previous.SrcObjectID != edge.SrcObjectID {
			if err := txn.Delete([]byte(kpEdgeSrcIdx + previous.SrcObjectID + "|" + edge.EdgeID)); err != nil {
				return err
			}
		}
		if previous.DstObjectID != edge.DstObjectID {
			if err := txn.Delete([]byte(kpEdgeDstIdx + previous.DstObjectID + "|" + edge.EdgeID)); err != nil {
				return err
			}
		}
	} else if err != badger.ErrKeyNotFound {
		return err
	}
	if err := setJSONTxn(txn, edgeKey, edge); err != nil {
		return err
	}
	if err := txn.Set([]byte(kpEdgeSrcIdx+edge.SrcObjectID+"|"+edge.EdgeID), edgeKey); err != nil {
		return err
	}
	return txn.Set([]byte(kpEdgeDstIdx+edge.DstObjectID+"|"+edge.EdgeID), edgeKey)
}

func putVersionTxn(txn *badger.Txn, version schemas.ObjectVersion) (bool, error) {
	key := verKey(version.ObjectID)
	var list []schemas.ObjectVersion
	item, err := txn.Get(key)
	if err == nil {
		if err := item.Value(func(value []byte) error { return json.Unmarshal(value, &list) }); err != nil {
			return false, err
		}
	} else if err != badger.ErrKeyNotFound {
		return false, err
	}
	for index, existing := range list {
		if sameObjectVersion(existing, version) {
			list[index] = version
			return false, setJSONTxn(txn, key, list)
		}
	}
	list = append(list, version)
	return true, setJSONTxn(txn, key, list)
}
