package worker

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"andb/src/internal/coordinator"
	"andb/src/internal/dataplane"
	"andb/src/internal/eventbackbone"
	"andb/src/internal/evidence"
	"andb/src/internal/materialization"
	"andb/src/internal/schemas"
	"andb/src/internal/semantic"
	"andb/src/internal/storage"
	"andb/src/internal/worker/chain"
	"andb/src/internal/worker/nodes"
)

type Runtime struct {
	wal               eventbackbone.WAL
	bus               eventbackbone.Bus
	plane             dataplane.DataPlane
	coord             *coordinator.Hub
	policy            *semantic.PolicyEngine
	planner           semantic.QueryPlanner
	materializer      *materialization.Service
	preCompute        *materialization.PreComputeService
	assembler         *evidence.Assembler
	evCache           *evidence.Cache
	derivationLog     *eventbackbone.DerivationLog
	policyDecisionLog *eventbackbone.PolicyDecisionLog
	nodeManager       *nodes.Manager
	storage           storage.RuntimeStorage
	tieredObjects     *storage.TieredObjectStore
	queryChain        *chain.QueryChain
}

func CreateRuntime(
	wal eventbackbone.WAL,
	bus eventbackbone.Bus,
	plane dataplane.DataPlane,
	coord *coordinator.Hub,
	policy *semantic.PolicyEngine,
	planner semantic.QueryPlanner,
	materializer *materialization.Service,
	preCompute *materialization.PreComputeService,
	assembler *evidence.Assembler,
	evCache *evidence.Cache,
	derivationLog *eventbackbone.DerivationLog,
	policyDecisionLog *eventbackbone.PolicyDecisionLog,
	nodeManager *nodes.Manager,
	store storage.RuntimeStorage,
	tieredObjs *storage.TieredObjectStore,
) *Runtime {
	return &Runtime{
		wal:               wal,
		bus:               bus,
		plane:             plane,
		coord:             coord,
		policy:            policy,
		planner:           planner,
		materializer:      materializer,
		preCompute:        preCompute,
		assembler:         assembler,
		evCache:           evCache,
		derivationLog:     derivationLog,
		policyDecisionLog: policyDecisionLog,
		nodeManager:       nodeManager,
		storage:           store,
		tieredObjects:     tieredObjs,
		queryChain:        chain.CreateQueryChain(nodeManager),
	}
}

func (r *Runtime) RegisterDefaults() {
	_ = r.bus.Subscribe("wal.events")
}

// QueryChain returns the post-retrieval reasoning chain (ProofTrace + Subgraph).
// It is nil if the Runtime was constructed without a nodeManager.
func (r *Runtime) QueryChain() *chain.QueryChain {
	return r.queryChain
}

// StartSubscriber launches the EventSubscriber's poll loop as a background
// goroutine tied to ctx.  The goroutine exits cleanly when ctx is cancelled.
func (r *Runtime) StartSubscriber(ctx context.Context, sub *EventSubscriber) {
	go sub.Run(ctx)
}

func (r *Runtime) SubmitIngest(ev schemas.Event) (map[string]any, error) {
	if strings.TrimSpace(ev.EventID) == "" {
		return nil, errors.New("event_id is required")
	}
	// IngestWorker validation: runs all registered IngestWorkers before WAL
	// append so malformed events are rejected before touching durable state.
	if err := r.nodeManager.DispatchIngestValidation(ev); err != nil {
		return nil, err
	}
	entry, err := r.wal.Append(ev)
	if err != nil {
		return nil, err
	}
	if ev.LogicalTS == 0 {
		ev.LogicalTS = entry.LSN
	}
	mat := r.materializer.MaterializeEvent(ev)
	record := mat.Record

	// ── Persist canonical objects ─────────────────────────────────────────
	// Route Memory writes through TieredObjectStore so the hot/warm/cold tiers
	// are kept in sync and cold queries (IncludeCold=true) can find results.
	if r.tieredObjects != nil {
		// Compute salience from the event importance if available, default 0.5.
		salience := mat.Memory.Importance
		if salience <= 0 {
			salience = 0.5
		}
		r.tieredObjects.PutMemory(mat.Memory, salience)
		r.tieredObjects.ArchiveColdRecord(
			mat.Memory.MemoryID,
			record.Text,
			record.Attributes,
			record.Namespace,
			record.EventUnixTS,
		)
	} else {
		// Fallback for tests or code paths that don't initialise TieredObjectStore.
		r.storage.Objects().PutMemory(mat.Memory)
	}
	r.storage.Versions().PutVersion(mat.Version)
	for _, edge := range mat.Edges {
		r.storage.Edges().PutEdge(edge)
	}

	// ── Pre-compute evidence fragment ─────────────────────────────────────
	if r.preCompute != nil {
		frag := r.preCompute.Compute(ev, record)
		if frag.SalienceScore >= 0.5 {
			r.storage.HotCache().Put(record.ObjectID, ev.EventType, record, frag.SalienceScore)
		}
	}

	// ── Retrieval plane ───────────────────────────────────────────────────
	r.nodeManager.DispatchIngest(record)
	if err := r.plane.Ingest(record); err != nil {
		return nil, err
	}
	return map[string]any{
		"status":    "accepted",
		"lsn":       entry.LSN,
		"event_id":  ev.EventID,
		"memory_id": mat.Memory.MemoryID,
		"edges":     len(mat.Edges),
	}, nil
}

func (r *Runtime) ExecuteQuery(req schemas.QueryRequest) schemas.QueryResponse {
	plan := r.planner.Build(req)
	searchInput := dataplane.SearchInput{
		QueryText:      req.QueryText,
		TopK:           plan.TopK,
		Namespace:      plan.Namespace,
		Constraints:    plan.Constraints,
		TimeFromUnixTS: plan.TimeFromUnixTS,
		TimeToUnixTS:   plan.TimeToUnixTS,
		IncludeGrowing: plan.IncludeGrowing,
		ObjectTypes:    plan.ObjectTypes,
		MemoryTypes:    plan.MemoryTypes,
	}
	result := r.nodeManager.DispatchQuery(searchInput, r.plane)
	result.ObjectIDs = semantic.FilterObjectIDsByTypes(result.ObjectIDs, plan.ObjectTypes)
	filters := r.policy.ApplyQueryFilters(req)
	resp := r.assembler.Build(searchInput, result, filters)

	// ── Post-retrieval reasoning via QueryChain ───────────────────────────────
	// QueryChain handles:
	//   1. Pre-fetching Memory objects as GraphNodes for node population.
	//   2. Pre-fetching BulkEdges for edge pre-population.
	//   3. Multi-hop BFS proof trace via ProofTraceWorker.
	//   4. Subgraph expansion via SubgraphExecutorWorker.
	//   5. Merging subgraph edges with the assembler's edges (deduplicated).
	if len(result.ObjectIDs) > 0 {
		chainOut, chainResult := r.queryChain.Run(chain.QueryChainInput{
			ObjectIDs:    result.ObjectIDs,
			MaxDepth:     0, // default cap of 8
			ObjectStore:  r.storage.Objects(),
			EdgeStore:    r.storage.Edges(),
		})
		_ = chainResult // chainResult.OK is advisory; non-fatal

		if len(chainOut.ProofTrace) > 0 {
			resp.ProofTrace = append(resp.ProofTrace, chainOut.ProofTrace...)
		}

		// Merge subgraph-expanded edges into resp.Edges, deduplicating by EdgeID.
		if len(chainOut.MergedEdges) > 0 {
			seen := make(map[string]bool, len(resp.Edges))
			for _, e := range resp.Edges {
				seen[e.EdgeID] = true
			}
			for _, e := range chainOut.MergedEdges {
				if !seen[e.EdgeID] {
					resp.Edges = append(resp.Edges, e)
					seen[e.EdgeID] = true
				}
			}
		}
	}

	return resp
}

func (r *Runtime) Topology() map[string]any {
	return map[string]any{
		"nodes":    r.nodeManager.Topology(),
		"segments": r.storage.Segments().List(""),
		"indexes":  r.storage.Indexes().List(),
	}
}

// GetEvidenceFragment returns the pre-computed EvidenceFragment for an object ID
// from the hot cache. Returns nil if not cached.
func (r *Runtime) GetEvidenceFragment(objectID string) any {
	if r.evCache == nil {
		return nil
	}
	if frag, ok := r.evCache.Get(objectID); ok {
		return frag
	}
	return nil
}

// GetDerivationLog returns derivation chain entries for the given object ID
// from the append-only DerivationLog.
func (r *Runtime) GetDerivationLog(objectID string) []string {
	if r.derivationLog == nil {
		return nil
	}
	entries := r.derivationLog.ForDerived(objectID)
	if len(entries) == 0 {
		return nil
	}
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = fmt.Sprintf("lsn=%d op=%s src=%s(%s) → %s(%s)",
			e.LSN, e.Operation, e.SourceID, e.SourceType, e.DerivedID, e.DerivedType)
	}
	return out
}

// GetPolicyDecisions returns governance decision entries for the given object ID
// from the append-only PolicyDecisionLog.
func (r *Runtime) GetPolicyDecisions(objectID string) []string {
	if r.policyDecisionLog == nil {
		return nil
	}
	entries := r.policyDecisionLog.ForObject(objectID)
	if len(entries) == 0 {
		return nil
	}
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = fmt.Sprintf("lsn=%d decision=%s policy=%s reason=%s",
			e.LSN, e.Decision, e.PolicyID, e.Reason)
	}
	return out
}
