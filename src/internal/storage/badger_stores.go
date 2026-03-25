package storage

import (
	"encoding/json"
	"time"

	"andb/src/internal/schemas"

	"github.com/dgraph-io/badger/v4"
)

// Key prefixes (ASCII, stable) for Badger key-value layout.
const (
	kpSeg        = "seg|"
	kpIdx        = "idx|"
	kpObjAgent    = "obj|agent|"
	kpObjSession  = "obj|session|"
	kpObjMemory   = "obj|memory|"
	kpObjState    = "obj|state|"
	kpObjArtifact = "obj|artifact|"
	kpObjEvent    = "obj|event|"
	kpObjUser     = "obj|user|"
	kpEdge        = "edg|"
	kpVer         = "ver|"
	kpPol         = "pol|"
	kpCtr         = "ctr|"
)

// ─── SegmentStore ────────────────────────────────────────────────────────────

type badgerSegmentStore struct{ db *badger.DB }

func newBadgerSegmentStore(db *badger.DB) *badgerSegmentStore {
	return &badgerSegmentStore{db: db}
}

func segKey(rec SegmentRecord) []byte {
	key := rec.Namespace + ":" + rec.SegmentID
	return []byte(kpSeg + key)
}

func (s *badgerSegmentStore) Upsert(record SegmentRecord) {
	record.UpdatedAt = time.Now().UTC()
	_ = badgerSetJSON(s.db, segKey(record), record)
}

func (s *badgerSegmentStore) List(namespace string) []SegmentRecord {
	var out []SegmentRecord
	_ = s.db.View(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		prefix := []byte(kpSeg)
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			item := it.Item()
			_ = item.Value(func(val []byte) error {
				var rec SegmentRecord
				if err := json.Unmarshal(val, &rec); err != nil {
					return err
				}
				if namespace == "" || rec.Namespace == namespace {
					out = append(out, rec)
				}
				return nil
			})
		}
		return nil
	})
	return out
}

// ─── IndexStore ──────────────────────────────────────────────────────────────

type badgerIndexStore struct{ db *badger.DB }

func newBadgerIndexStore(db *badger.DB) *badgerIndexStore {
	return &badgerIndexStore{db: db}
}

func (s *badgerIndexStore) Upsert(record IndexRecord) {
	record.UpdatedAt = time.Now().UTC()
	key := []byte(kpIdx + record.Namespace)
	_ = badgerSetJSON(s.db, key, record)
}

func (s *badgerIndexStore) List() []IndexRecord {
	var out []IndexRecord
	_ = s.db.View(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		prefix := []byte(kpIdx)
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			item := it.Item()
			_ = item.Value(func(val []byte) error {
				var rec IndexRecord
				if err := json.Unmarshal(val, &rec); err != nil {
					return err
				}
				out = append(out, rec)
				return nil
			})
		}
		return nil
	})
	return out
}

// ─── ObjectStore ─────────────────────────────────────────────────────────────

type badgerObjectStore struct{ db *badger.DB }

func newBadgerObjectStore(db *badger.DB) *badgerObjectStore {
	return &badgerObjectStore{db: db}
}

func (s *badgerObjectStore) PutAgent(obj schemas.Agent) {
	_ = badgerSetJSON(s.db, []byte(kpObjAgent+obj.AgentID), obj)
}
func (s *badgerObjectStore) GetAgent(id string) (schemas.Agent, bool) {
	var o schemas.Agent
	ok, err := badgerGetJSON(s.db, []byte(kpObjAgent+id), &o)
	return o, ok && err == nil
}
func (s *badgerObjectStore) ListAgents() []schemas.Agent {
	return listByPrefix[schemas.Agent](s.db, kpObjAgent)
}

func (s *badgerObjectStore) PutSession(obj schemas.Session) {
	_ = badgerSetJSON(s.db, []byte(kpObjSession+obj.SessionID), obj)
}
func (s *badgerObjectStore) GetSession(id string) (schemas.Session, bool) {
	var o schemas.Session
	ok, err := badgerGetJSON(s.db, []byte(kpObjSession+id), &o)
	return o, ok && err == nil
}
func (s *badgerObjectStore) ListSessions(agentID string) []schemas.Session {
	all := listByPrefix[schemas.Session](s.db, kpObjSession)
	if agentID == "" {
		return all
	}
	out := make([]schemas.Session, 0)
	for _, v := range all {
		if v.AgentID == agentID {
			out = append(out, v)
		}
	}
	return out
}

func (s *badgerObjectStore) PutMemory(obj schemas.Memory) {
	_ = badgerSetJSON(s.db, []byte(kpObjMemory+obj.MemoryID), obj)
}
func (s *badgerObjectStore) GetMemory(id string) (schemas.Memory, bool) {
	var o schemas.Memory
	ok, err := badgerGetJSON(s.db, []byte(kpObjMemory+id), &o)
	return o, ok && err == nil
}
func (s *badgerObjectStore) ListMemories(agentID, sessionID string) []schemas.Memory {
	all := listByPrefix[schemas.Memory](s.db, kpObjMemory)
	out := make([]schemas.Memory, 0)
	for _, v := range all {
		if (agentID == "" || v.AgentID == agentID) &&
			(sessionID == "" || v.SessionID == sessionID) {
			out = append(out, v)
		}
	}
	return out
}

func (s *badgerObjectStore) PutState(obj schemas.State) {
	_ = badgerSetJSON(s.db, []byte(kpObjState+obj.StateID), obj)
}
func (s *badgerObjectStore) GetState(id string) (schemas.State, bool) {
	var o schemas.State
	ok, err := badgerGetJSON(s.db, []byte(kpObjState+id), &o)
	return o, ok && err == nil
}
func (s *badgerObjectStore) ListStates(agentID, sessionID string) []schemas.State {
	all := listByPrefix[schemas.State](s.db, kpObjState)
	out := make([]schemas.State, 0)
	for _, v := range all {
		if (agentID == "" || v.AgentID == agentID) &&
			(sessionID == "" || v.SessionID == sessionID) {
			out = append(out, v)
		}
	}
	return out
}

func (s *badgerObjectStore) PutArtifact(obj schemas.Artifact) {
	_ = badgerSetJSON(s.db, []byte(kpObjArtifact+obj.ArtifactID), obj)
}
func (s *badgerObjectStore) GetArtifact(id string) (schemas.Artifact, bool) {
	var o schemas.Artifact
	ok, err := badgerGetJSON(s.db, []byte(kpObjArtifact+id), &o)
	return o, ok && err == nil
}
func (s *badgerObjectStore) ListArtifacts(sessionID string) []schemas.Artifact {
	all := listByPrefix[schemas.Artifact](s.db, kpObjArtifact)
	if sessionID == "" {
		return all
	}
	out := make([]schemas.Artifact, 0)
	for _, v := range all {
		if v.SessionID == sessionID {
			out = append(out, v)
		}
	}
	return out
}

func (s *badgerObjectStore) PutEvent(obj schemas.Event) {
	_ = badgerSetJSON(s.db, []byte(kpObjEvent+obj.EventID), obj)
}
func (s *badgerObjectStore) GetEvent(id string) (schemas.Event, bool) {
	var o schemas.Event
	ok, err := badgerGetJSON(s.db, []byte(kpObjEvent+id), &o)
	return o, ok && err == nil
}
func (s *badgerObjectStore) ListEvents(agentID, sessionID string) []schemas.Event {
	all := listByPrefix[schemas.Event](s.db, kpObjEvent)
	if agentID == "" && sessionID == "" {
		return all
	}
	out := make([]schemas.Event, 0)
	for _, v := range all {
		if (agentID == "" || v.AgentID == agentID) &&
			(sessionID == "" || v.SessionID == sessionID) {
			out = append(out, v)
		}
	}
	return out
}

func (s *badgerObjectStore) PutUser(obj schemas.User) {
	_ = badgerSetJSON(s.db, []byte(kpObjUser+obj.UserID), obj)
}
func (s *badgerObjectStore) GetUser(id string) (schemas.User, bool) {
	var o schemas.User
	ok, err := badgerGetJSON(s.db, []byte(kpObjUser+id), &o)
	return o, ok && err == nil
}
func (s *badgerObjectStore) ListUsers() []schemas.User {
	return listByPrefix[schemas.User](s.db, kpObjUser)
}

func listByPrefix[T any](db *badger.DB, prefix string) []T {
	var out []T
	_ = db.View(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		p := []byte(prefix)
		for it.Seek(p); it.ValidForPrefix(p); it.Next() {
			item := it.Item()
			_ = item.Value(func(val []byte) error {
				var v T
				if err := json.Unmarshal(val, &v); err != nil {
					return err
				}
				out = append(out, v)
				return nil
			})
		}
		return nil
	})
	return out
}

// ─── GraphEdgeStore ──────────────────────────────────────────────────────────

type badgerGraphEdgeStore struct{ db *badger.DB }

func newBadgerGraphEdgeStore(db *badger.DB) *badgerGraphEdgeStore {
	return &badgerGraphEdgeStore{db: db}
}

func (s *badgerGraphEdgeStore) PutEdge(edge schemas.Edge) {
	_ = badgerSetJSON(s.db, []byte(kpEdge+edge.EdgeID), edge)
}

func (s *badgerGraphEdgeStore) GetEdge(id string) (schemas.Edge, bool) {
	var e schemas.Edge
	ok, err := badgerGetJSON(s.db, []byte(kpEdge+id), &e)
	return e, ok && err == nil
}

func (s *badgerGraphEdgeStore) DeleteEdge(id string) {
	_ = badgerDelete(s.db, []byte(kpEdge+id))
}

func (s *badgerGraphEdgeStore) allEdges() []schemas.Edge {
	return listByPrefix[schemas.Edge](s.db, kpEdge)
}

func (s *badgerGraphEdgeStore) EdgesFrom(srcObjectID string) []schemas.Edge {
	out := make([]schemas.Edge, 0)
	for _, e := range s.allEdges() {
		if e.SrcObjectID == srcObjectID {
			out = append(out, e)
		}
	}
	return out
}

func (s *badgerGraphEdgeStore) EdgesTo(dstObjectID string) []schemas.Edge {
	out := make([]schemas.Edge, 0)
	for _, e := range s.allEdges() {
		if e.DstObjectID == dstObjectID {
			out = append(out, e)
		}
	}
	return out
}

func (s *badgerGraphEdgeStore) BulkEdges(objectIDs []string) []schemas.Edge {
	set := make(map[string]bool, len(objectIDs))
	for _, id := range objectIDs {
		set[id] = true
	}
	out := make([]schemas.Edge, 0)
	for _, e := range s.allEdges() {
		if set[e.SrcObjectID] || set[e.DstObjectID] {
			out = append(out, e)
		}
	}
	return out
}

func (s *badgerGraphEdgeStore) ListEdges() []schemas.Edge {
	return s.allEdges()
}

func (s *badgerGraphEdgeStore) PruneExpiredEdges(now string) int {
	var count int
	_ = s.db.Update(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		prefix := []byte(kpEdge)
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			item := it.Item()
			_ = item.Value(func(val []byte) error {
				var e schemas.Edge
				if err := json.Unmarshal(val, &e); err != nil {
					return err
				}
				if e.ExpiresAt != "" && e.ExpiresAt <= now {
					if err := txn.Delete(item.Key()); err == nil {
						count++
					}
				}
				return nil
			})
		}
		return nil
	})
	return count
}

// ─── SnapshotVersionStore ────────────────────────────────────────────────────

type badgerSnapshotVersionStore struct{ db *badger.DB }

func newBadgerSnapshotVersionStore(db *badger.DB) *badgerSnapshotVersionStore {
	return &badgerSnapshotVersionStore{db: db}
}

func verKey(objectID string) []byte {
	return []byte(kpVer + objectID)
}

func (s *badgerSnapshotVersionStore) PutVersion(v schemas.ObjectVersion) {
	_ = s.db.Update(func(txn *badger.Txn) error {
		key := verKey(v.ObjectID)
		var list []schemas.ObjectVersion
		item, err := txn.Get(key)
		if err == nil {
			_ = item.Value(func(val []byte) error {
				return json.Unmarshal(val, &list)
			})
		} else if err != badger.ErrKeyNotFound {
			return err
		}
		list = append(list, v)
		b, err := json.Marshal(list)
		if err != nil {
			return err
		}
		return txn.Set(key, b)
	})
}

func (s *badgerSnapshotVersionStore) GetVersions(objectID string) []schemas.ObjectVersion {
	var list []schemas.ObjectVersion
	ok, err := badgerGetJSON(s.db, verKey(objectID), &list)
	if !ok || err != nil {
		return nil
	}
	return append([]schemas.ObjectVersion{}, list...)
}

func (s *badgerSnapshotVersionStore) LatestVersion(objectID string) (schemas.ObjectVersion, bool) {
	vs := s.GetVersions(objectID)
	if len(vs) == 0 {
		return schemas.ObjectVersion{}, false
	}
	latest := vs[0]
	for _, v := range vs[1:] {
		if v.Version > latest.Version {
			latest = v
		}
	}
	return latest, true
}

// ─── PolicyStore ─────────────────────────────────────────────────────────────

type badgerPolicyStore struct{ db *badger.DB }

func newBadgerPolicyStore(db *badger.DB) *badgerPolicyStore {
	return &badgerPolicyStore{db: db}
}

func polKey(objectID string) []byte {
	return []byte(kpPol + objectID)
}

func (s *badgerPolicyStore) AppendPolicy(p schemas.PolicyRecord) {
	_ = s.db.Update(func(txn *badger.Txn) error {
		key := polKey(p.ObjectID)
		var list []schemas.PolicyRecord
		item, err := txn.Get(key)
		if err == nil {
			_ = item.Value(func(val []byte) error {
				return json.Unmarshal(val, &list)
			})
		} else if err != badger.ErrKeyNotFound {
			return err
		}
		list = append(list, p)
		b, err := json.Marshal(list)
		if err != nil {
			return err
		}
		return txn.Set(key, b)
	})
}

func (s *badgerPolicyStore) GetPolicies(objectID string) []schemas.PolicyRecord {
	var list []schemas.PolicyRecord
	ok, err := badgerGetJSON(s.db, polKey(objectID), &list)
	if !ok || err != nil {
		return nil
	}
	return append([]schemas.PolicyRecord{}, list...)
}

func (s *badgerPolicyStore) ListPolicies() []schemas.PolicyRecord {
	var out []schemas.PolicyRecord
	_ = s.db.View(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		prefix := []byte(kpPol)
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			item := it.Item()
			_ = item.Value(func(val []byte) error {
				var list []schemas.PolicyRecord
				if err := json.Unmarshal(val, &list); err != nil {
					return err
				}
				out = append(out, list...)
				return nil
			})
		}
		return nil
	})
	return out
}

// ─── ShareContractStore ──────────────────────────────────────────────────────

type badgerShareContractStore struct{ db *badger.DB }

func newBadgerShareContractStore(db *badger.DB) *badgerShareContractStore {
	return &badgerShareContractStore{db: db}
}

func (s *badgerShareContractStore) PutContract(c schemas.ShareContract) {
	_ = badgerSetJSON(s.db, []byte(kpCtr+c.ContractID), c)
}

func (s *badgerShareContractStore) GetContract(id string) (schemas.ShareContract, bool) {
	var c schemas.ShareContract
	ok, err := badgerGetJSON(s.db, []byte(kpCtr+id), &c)
	return c, ok && err == nil
}

func (s *badgerShareContractStore) ContractsByScope(scope string) []schemas.ShareContract {
	all := listByPrefix[schemas.ShareContract](s.db, kpCtr)
	out := make([]schemas.ShareContract, 0)
	for _, c := range all {
		if c.Scope == scope {
			out = append(out, c)
		}
	}
	return out
}

func (s *badgerShareContractStore) ListContracts() []schemas.ShareContract {
	return listByPrefix[schemas.ShareContract](s.db, kpCtr)
}
