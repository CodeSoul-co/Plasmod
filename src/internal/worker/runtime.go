package worker

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"os"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"plasmod/src/internal/coordinator"
	"plasmod/src/internal/dataplane"
	"plasmod/src/internal/eventbackbone"
	"plasmod/src/internal/evidence"
	"plasmod/src/internal/materialization"
	"plasmod/src/internal/metrics"
	"plasmod/src/internal/schemas"
	"plasmod/src/internal/semantic"
	"plasmod/src/internal/storage"
	"plasmod/src/internal/worker/chain"
	"plasmod/src/internal/worker/consistency"
	"plasmod/src/internal/worker/nodes"
)

var releaseOSMemory = debug.FreeOSMemory

var (
	ErrShareInvalid   = errors.New("invalid share request")
	ErrShareNotFound  = errors.New("share resource not found")
	ErrShareForbidden = errors.New("share forbidden")
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
	// lastMem tracks the most-recent memory ID per "agentID:sessionID" so ConflictMerge
	// can fire synchronously in SubmitIngest (not only async via subscriber).
	lastMem           map[string]string
	lastMemMu         sync.RWMutex
	stateProjectionMu sync.Mutex
	wipeMu            sync.Mutex

	consistencyMu         sync.Mutex
	consistencyController *consistency.Controller
	consistencyConfig     consistency.Config
	consistencyWatermark  consistency.WatermarkAdvancer
	subscriberMu          sync.Mutex
	subscribers           []*EventSubscriber

	// VectorOnlyMode disables graph expansion, policy enforcement, and provenance
	// tracking to create a pure vector-search baseline (Baseline 1).
	VectorOnlyMode bool

	// MinimalMode disables provenance attachment, policy enforcement, and
	// version recording while preserving graph search.  Corresponds to
	// "Plasmod stripped" baselines 3-B4 / 4-B4.
	MinimalMode bool

	// GovernanceDisabled suppresses TTL / quarantine / ACL enforcement when
	// true.  Used by 4-B4 to run Plasmod without the governance layer.
	GovernanceDisabled bool
	memoryBackend      *memoryBackendRouter

	// flushTicker drives the background index-rebuild goroutine.  By decoupling
	// flush from write, we eliminate the O(n²) rebuild storm that occurred when N
	// concurrent writes each triggered their own synchronous full-index rebuild.
	flushTicker   *time.Ticker
	flushStopCh   chan struct{}
	flushInterval time.Duration
	flushLoopOnce sync.Once
	flushDirty    atomic.Bool

	embeddingSpecMu sync.RWMutex
	embeddingSpec   storage.EmbeddingSpec
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
		lastMem:           make(map[string]string),
		memoryBackend:     newMemoryBackendRouterFromEnv(),
		consistencyConfig: consistency.DefaultConfig(),
	}
}

func (r *Runtime) RegisterDefaults() {
	// EventSubscriber consumes the WAL via Scan polling. Creating an unread
	// pubsub subscription here would eventually fill its buffer and push
	// backpressure into WAL append latency.
}

// TieredObjects returns the tiered object store used on the ingest path, or nil.
func (r *Runtime) TieredObjects() *storage.TieredObjectStore {
	if r == nil {
		return nil
	}
	return r.tieredObjects
}

func (r *Runtime) IngestVectorsToWarmSegment(segmentID string, objectIDs []string, vectors [][]float32) (int, error) {
	tp, ok := r.plane.(*dataplane.TieredDataPlane)
	if !ok {
		return 0, fmt.Errorf("tiered plane unavailable")
	}
	return tp.IngestVectorsToWarmSegment(segmentID, objectIDs, vectors)
}

func (r *Runtime) IngestVectorsToWarmSegmentWithType(
	segmentID string, objectIDs []string, vectors [][]float32,
	indexType string, nlist, nprobe, m, nbits int, sqType string,
) (int, error) {
	tp, ok := r.plane.(*dataplane.TieredDataPlane)
	if !ok {
		return 0, fmt.Errorf("tiered plane unavailable")
	}
	return tp.IngestVectorsToWarmSegmentWithType(segmentID, objectIDs, vectors, indexType, nlist, nprobe, m, nbits, sqType)
}

func (r *Runtime) IngestFlatVectorsToWarmSegment(segmentID string, objectIDs []string, flatVectors []float32, n, dim int) (int, error) {
	tp, ok := r.plane.(*dataplane.TieredDataPlane)
	if !ok {
		return 0, fmt.Errorf("tiered plane unavailable")
	}
	return tp.IngestFlatVectorsToWarmSegment(segmentID, objectIDs, flatVectors, n, dim)
}

func (r *Runtime) IngestFlatVectorsToWarmSegmentWithType(
	segmentID string, objectIDs []string, flatVectors []float32, n, dim int,
	indexType string, nlist, nprobe, m, nbits int, sqType string,
) (int, error) {
	tp, ok := r.plane.(*dataplane.TieredDataPlane)
	if !ok {
		return 0, fmt.Errorf("tiered plane unavailable")
	}
	return tp.IngestFlatVectorsToWarmSegmentWithType(segmentID, objectIDs, flatVectors, n, dim, indexType, nlist, nprobe, m, nbits, sqType)
}

func (r *Runtime) UnloadWarmSegment(segmentID string) error {
	tp, ok := r.plane.(*dataplane.TieredDataPlane)
	if !ok {
		return fmt.Errorf("tiered plane unavailable")
	}
	return tp.UnloadWarmSegment(segmentID)
}

func (r *Runtime) SearchWarmSegment(segmentID, queryText string, topK int, queryVec []float32) ([]string, error) {
	tp, ok := r.plane.(*dataplane.TieredDataPlane)
	if !ok {
		return nil, fmt.Errorf("tiered plane unavailable")
	}
	return tp.SearchWarmSegment(segmentID, queryText, topK, queryVec)
}

func (r *Runtime) RegisterWarmSegment(segmentID string, objectIDs []string) error {
	tp, ok := r.plane.(*dataplane.TieredDataPlane)
	if !ok {
		return fmt.Errorf("tiered plane unavailable")
	}
	return tp.RegisterWarmSegment(segmentID, objectIDs)
}

func (r *Runtime) SearchWarmSegmentBatch(segmentID string, nq int, topK int, queries []float32) ([]int64, []float32, error) {
	tp, ok := r.plane.(*dataplane.TieredDataPlane)
	if !ok {
		return nil, nil, fmt.Errorf("tiered plane unavailable")
	}
	return tp.SearchWarmSegmentBatch(segmentID, nq, topK, queries)
}

func (r *Runtime) SearchWarmSegmentSerialBatch(segmentID string, nq int, topK int, queries []float32) ([]int64, []float32, error) {
	tp, ok := r.plane.(*dataplane.TieredDataPlane)
	if !ok {
		return nil, nil, fmt.Errorf("tiered plane unavailable")
	}
	return tp.SearchWarmSegmentSerialBatch(segmentID, nq, topK, queries)
}

func (r *Runtime) SearchWarmSegmentBatchRaw(segmentID string, nq int, topK int, queries []float32) ([]int64, []float32, error) {
	tp, ok := r.plane.(*dataplane.TieredDataPlane)
	if !ok {
		return nil, nil, fmt.Errorf("tiered plane unavailable")
	}
	return tp.SearchWarmSegmentBatchRaw(segmentID, nq, topK, queries)
}

// SearchWarmSegmentBatchObjectIDs runs warm batch ANN and maps hits to registered object id strings.
func (r *Runtime) SearchWarmSegmentBatchObjectIDs(segmentID string, nq int, topK int, queries []float32, raw bool) ([][]string, [][]float32, error) {
	tp, ok := r.plane.(*dataplane.TieredDataPlane)
	if !ok {
		return nil, nil, fmt.Errorf("tiered plane unavailable")
	}
	return tp.SearchWarmSegmentBatchObjectIDs(segmentID, nq, topK, queries, raw)
}

func (r *Runtime) AdminWarmPrebuild() error {
	if r.plane == nil {
		return fmt.Errorf("data plane unavailable")
	}
	return r.plane.Flush()
}

// ConfigureEmbeddingSpec records the actual vector space selected at bootstrap.
// It is stamped onto every subsequently persisted retrieval segment.
func (r *Runtime) ConfigureEmbeddingSpec(spec storage.EmbeddingSpec) error {
	if !spec.Valid() {
		return fmt.Errorf("invalid embedding spec %s", spec)
	}
	r.embeddingSpecMu.Lock()
	r.embeddingSpec = spec
	r.embeddingSpecMu.Unlock()
	return nil
}

func (r *Runtime) EmbeddingSpec() storage.EmbeddingSpec {
	if r == nil {
		return storage.EmbeddingSpec{}
	}
	r.embeddingSpecMu.RLock()
	defer r.embeddingSpecMu.RUnlock()
	return r.embeddingSpec
}

func (r *Runtime) stampEmbeddingSpec(record *dataplane.IngestRecord) {
	if record == nil {
		return
	}
	spec := r.EmbeddingSpec()
	if !spec.Valid() {
		return
	}
	record.EmbeddingFamily = spec.Family
	record.EmbeddingDim = spec.Dim
}

// ReindexEmbeddings rebuilds hot/warm retrieval state from canonical memories
// using the configured embedding provider. It intentionally does not replay
// the WAL or alter canonical objects.
func (r *Runtime) ReindexEmbeddings() (int, error) {
	if r == nil || r.storage == nil {
		return 0, fmt.Errorf("runtime storage unavailable")
	}
	spec := r.EmbeddingSpec()
	if !spec.Valid() {
		return 0, fmt.Errorf("embedding spec is not configured")
	}
	tp, ok := r.plane.(*dataplane.TieredDataPlane)
	if !ok {
		return 0, fmt.Errorf("embedding reindex requires tiered data plane")
	}

	r.wipeMu.Lock()
	defer r.wipeMu.Unlock()
	if err := tp.RebuildEmbeddingIndex(); err != nil {
		return 0, err
	}

	warmMemories := r.storage.Objects().ListMemories("", "")
	warmByID := make(map[string]schemas.Memory, len(warmMemories))
	for _, memory := range warmMemories {
		warmByID[memory.MemoryID] = memory
	}
	coldByID := map[string]schemas.Memory{}
	if r.tieredObjects != nil {
		for _, memory := range r.tieredObjects.ListColdMemories() {
			coldByID[memory.MemoryID] = memory
		}
	}
	segments := r.storage.Segments().List("")
	records := make([]dataplane.IngestRecord, 0, len(segments))
	for _, segment := range segments {
		memoryID := segment.StorageRef
		if memoryID == "" {
			memoryID = segment.SegmentID
		}
		memory, ok := warmByID[memoryID]
		if !ok {
			if _, archived := coldByID[memoryID]; archived {
				// Archived memories are re-embedded in the cold tier below and
				// must not be promoted back into the warm retrieval index.
				continue
			}
			return 0, fmt.Errorf("cannot reindex segment %q: canonical memory %q is missing", segment.SegmentID, memoryID)
		}
		text := strings.TrimSpace(memory.Content)
		if text == "" {
			text = strings.TrimSpace(memory.Summary)
		}
		if text == "" {
			return 0, fmt.Errorf("cannot reindex segment %q: canonical memory %q has no content", segment.SegmentID, memoryID)
		}
		namespace := segment.Namespace
		if namespace == "" {
			namespace = "default"
		}
		records = append(records, dataplane.IngestRecord{
			ObjectID:        memoryID,
			Text:            text,
			Namespace:       namespace,
			Attributes:      map[string]string{"embedding_family": spec.Family},
			EmbeddingFamily: spec.Family,
			EmbeddingDim:    spec.Dim,
		})
	}
	if err := tp.BatchIngest(records); err != nil {
		return 0, fmt.Errorf("reindex embeddings: %w", err)
	}
	if err := tp.Flush(); err != nil {
		return 0, fmt.Errorf("build reindexed embedding index: %w", err)
	}
	coldCount := 0
	if r.tieredObjects != nil {
		var err error
		coldCount, err = r.tieredObjects.ReindexColdEmbeddings()
		if err != nil {
			return 0, err
		}
	}
	for _, segment := range segments {
		segment.EmbeddingFamily = spec.Family
		segment.EmbeddingDim = spec.Dim
		r.storage.Segments().Upsert(segment)
	}
	r.flushDirty.Store(false)
	return len(records) + coldCount, nil
}

// QueryChain returns the post-retrieval reasoning chain (ProofTrace + Subgraph).
// It is nil if the Runtime was constructed without a nodeManager.
func (r *Runtime) QueryChain() *chain.QueryChain {
	return r.queryChain
}

// StartSubscriber launches the EventSubscriber's poll loop as a background
// goroutine tied to ctx.  The goroutine exits cleanly when ctx is cancelled.
func (r *Runtime) StartSubscriber(ctx context.Context, sub *EventSubscriber) {
	if sub == nil || visibilityOnlyModeEnabled() {
		return
	}
	sub.SetVisibilityBoundary(func() int64 {
		return r.ConsistencyStatus().VisibleWatermark
	})
	r.subscriberMu.Lock()
	r.subscribers = append(r.subscribers, sub)
	r.subscriberMu.Unlock()
	go sub.Run(ctx)
}

func (r *Runtime) pauseSubscribers() []*EventSubscriber {
	r.subscriberMu.Lock()
	subscribers := append([]*EventSubscriber(nil), r.subscribers...)
	r.subscriberMu.Unlock()
	for _, subscriber := range subscribers {
		subscriber.Pause()
	}
	return subscribers
}

func resetSubscribers(subscribers []*EventSubscriber) {
	for _, subscriber := range subscribers {
		subscriber.Reset()
	}
}

func resumeSubscribers(subscribers []*EventSubscriber) {
	for _, subscriber := range subscribers {
		subscriber.Resume()
	}
}

// StartMemoryDeleteOutbox is a no-op (deleteOutbox removed)
func (r *Runtime) StartMemoryDeleteOutbox(ctx context.Context) {}

// resolveFlushInterval reads PLASMOD_FLUSH_INTERVAL from the environment.
// Default: 5 seconds.  A value of 0 disables the background flush loop.
func resolveFlushInterval() time.Duration {
	const defaultInterval = 5 * time.Second
	raw := strings.TrimSpace(os.Getenv("PLASMOD_FLUSH_INTERVAL"))
	if raw == "" {
		return defaultInterval
	}
	if raw == "0" || strings.EqualFold(raw, "off") || strings.EqualFold(raw, "none") {
		return 0
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return defaultInterval
	}
	return d
}

// StartFlushLoop launches a background goroutine that rebuilds the retrieval index
// at a fixed interval, completely decoupled from the write path.  This prevents
// the O(n²) rebuild storm that occurred when N concurrent writes each triggered
// their own synchronous full-index rebuild.  The goroutine exits when ctx is
// cancelled.
func (r *Runtime) StartFlushLoop(ctx context.Context) {
	if r == nil || r.plane == nil {
		return
	}
	r.flushInterval = resolveFlushInterval()
	if r.flushInterval == 0 {
		return // disabled via PLASMOD_FLUSH_INTERVAL=0
	}
	r.flushLoopOnce.Do(func() {
		r.flushStopCh = make(chan struct{})
		r.flushTicker = time.NewTicker(r.flushInterval)
		go func() {
			defer r.flushTicker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-r.flushTicker.C:
					if !r.flushDirty.CompareAndSwap(true, false) {
						continue
					}
					if err := r.plane.Flush(); err != nil {
						r.flushDirty.Store(true)
						log.Printf("[flush-loop] periodic flush failed: %v", err)
					}
				case <-r.flushStopCh:
					return
				}
			}
		}()
	})
}

func (r *Runtime) projectWALEntry(ctx context.Context, entry eventbackbone.WALEntry) (map[string]any, error) {
	ev := entry.Event.NormalizeDynamicEventV04()
	if existing, ok := r.storage.Objects().GetEvent(ev.Identity.EventID); ok {
		existing = existing.NormalizeDynamicEventV04()
		if existing.Time.WalLSN > 0 && existing.Time.WalLSN != entry.LSN {
			memoryID := schemas.CanonicalMemoryID(existing)
			return map[string]any{
				"status":       "duplicate",
				"lsn":          entry.LSN,
				"original_lsn": existing.Time.WalLSN,
				"event_id":     existing.Identity.EventID,
				"memory_id":    memoryID,
			}, nil
		}
	}
	mat := r.materializer.MaterializeEvent(ev)
	record := mat.Record
	r.stampEmbeddingSpec(&record)

	// ── Persist canonical objects ─────────────────────────────────────────
	// Canonical state is authoritative and is committed before the disposable
	// retrieval projection. A failed retrieval write is retried from WAL; query
	// visibility is still gated by the Controller's visible watermark.
	r.stateProjectionMu.Lock()
	state, stateVersions := r.prepareStateMutation(mat.State, mat.StateVersion)
	mat.State = state
	projection := storage.CanonicalProjection{
		Event:                  &ev,
		Memory:                 &mat.Memory,
		State:                  state,
		Edges:                  mat.Edges,
		IncludeEventBaseEdges:  true,
		IncludeMemoryBaseEdges: true,
	}
	if !r.MinimalMode {
		projection.Versions = append(projection.Versions, mat.Version)
		projection.Versions = append(projection.Versions, stateVersions...)
	}
	if mat.Artifact != nil {
		projection.Artifact = mat.Artifact
		projection.IncludeArtifactBaseEdges = true
		if !r.MinimalMode && mat.ArtifactVersion != nil {
			projection.Versions = append(projection.Versions, *mat.ArtifactVersion)
		}
	}
	if err := r.storage.ApplyCanonicalProjection(projection); err != nil {
		r.stateProjectionMu.Unlock()
		return nil, err
	}
	r.stateProjectionMu.Unlock()

	if err := r.plane.Ingest(record); err != nil {
		return nil, err
	}
	r.flushDirty.Store(true)
	salience := mat.Memory.Importance
	if salience <= 0 {
		salience = 0.5
	}
	if r.tieredObjects != nil {
		r.tieredObjects.PromoteMemory(mat.Memory, salience)
	} else if r.storage.HotCache() != nil && salience >= schemas.DefaultAlgorithmConfig().HotTierSalienceThreshold {
		r.storage.HotCache().Put(mat.Memory.MemoryID, "memory", mat.Memory, salience)
	}

	// The ingest checkpoint State is part of the canonical commit above.
	// Specialized keyed-state, tool-trace, and derivation workers remain on the
	// asynchronous chains and are not included in the ingest visibility ACK.
	if r.memoryBackend != nil && r.memoryBackend.ShouldShadowWrite() {
		if err := r.memoryBackend.WriteShadow(ctx, mat.Memory, ev); err != nil {
			log.Printf("[memory-backend] shadow_write failed memory=%s: %v", mat.Memory.MemoryID, err)
		}
	}
	// ── Synchronous ConflictMerge ──────────────────────────────────────────
	// Detect and resolve same-session memory conflicts immediately after the new
	// memory is stored.  The async subscriber also fires ConflictMerge (for
	// cross-event races); this synchronous pass ensures the conflict_resolved
	// edge is present before SubmitIngest returns — critical for test queries
	// and any caller that reads edges immediately after ingest.
	if mat.Memory.AgentID != "" &&
		mat.Memory.SessionID != "" &&
		mat.Memory.MemoryType == string(schemas.MemoryTypeEpisodic) &&
		!visibilityOnlyModeEnabled() &&
		!shouldSkipConflictMergeForEvent(ev) {
		key := mat.Memory.AgentID + ":" + mat.Memory.SessionID
		r.lastMemMu.RLock()
		prevID, hasPrev := r.lastMem[key]
		r.lastMemMu.RUnlock()
		if hasPrev {
			r.nodeManager.DispatchConflictMerge(mat.Memory.MemoryID, prevID, string(schemas.ObjectTypeMemory))
		}
		r.lastMemMu.Lock()
		r.lastMem[key] = mat.Memory.MemoryID
		r.lastMemMu.Unlock()
	}

	// ── Pre-compute evidence fragment ─────────────────────────────────────
	if r.preCompute != nil && !visibilityOnlyModeEnabled() {
		frag := r.preCompute.Compute(ev, record)
		if frag.SalienceScore >= 0.5 {
			r.storage.HotCache().Put(record.ObjectID, ev.EventInfo.EventType, record, frag.SalienceScore)
		}
	}

	// ── Retrieval plane ───────────────────────────────────────────────────
	if !visibilityOnlyModeEnabled() {
		r.nodeManager.DispatchIngest(record)
	}
	if ev.Time.IngestTime > 0 {
		metrics.Global().RecordWriteToVisible(time.Since(time.UnixMilli(ev.Time.IngestTime)))
	}
	if ev.Actor.SessionID != "" {
		metrics.Global().Session(ev.Actor.SessionID).AddStep()
		metrics.Global().StorageMemoryCount.Add(1)
		metrics.Global().StorageEventCount.Add(1)
	}
	ack := map[string]any{
		"status":    "accepted",
		"lsn":       entry.LSN,
		"event_id":  ev.Identity.EventID,
		"memory_id": mat.Memory.MemoryID,
		"edges":     len(mat.Edges),
	}
	if len(mat.Edges) > 0 {
		edgeIDs := make([]string, 0, len(mat.Edges))
		for _, edge := range mat.Edges {
			if strings.TrimSpace(edge.EdgeID) != "" {
				edgeIDs = append(edgeIDs, edge.EdgeID)
			}
		}
		if len(edgeIDs) > 0 {
			ack["edge_ids"] = edgeIDs
		}
	}
	if mat.Artifact != nil {
		ack["artifact_id"] = mat.Artifact.ArtifactID
	}
	if mat.State != nil {
		ack["state_id"] = mat.State.StateID
		ack["state_version"] = mat.State.Version
	}
	return ack, nil
}

func (r *Runtime) executeQuery(req schemas.QueryRequest, readWatermarkLSN int64) schemas.QueryResponse {
	t0Query := time.Now()
	metrics.Global().ConcurrentQueries.Add(1)
	defer func() {
		metrics.Global().ConcurrentQueries.Add(-1)
		metrics.Global().RecordQueryLatency(time.Since(t0Query))
	}()
	if req.ResponseMode == schemas.ResponseModeObjectsOnly && len(req.TargetObjectIDs) > 0 {
		objectIDs := r.fetchTargetObjectIDs(req)
		objectIDs = filterObjectIDsExcludingInactiveMemories(r.storage.Objects(), objectIDs, nil)
		objectIDs, accessDecisions := r.filterObjectIDsByAccess(req, objectIDs, readWatermarkLSN)
		resp := schemas.QueryResponse{
			Objects:          objectIDs,
			AccessDecisions:  accessDecisions,
			ReadWatermarkLSN: readWatermarkLSN,
			Retrieval: &schemas.RetrievalSummary{
				Tier:          "target_object",
				RetrievalHits: len(objectIDs),
				CanonicalAdds: 0,
			},
			ChainTraces: schemas.ChainTraceSlots{
				Main:           []string{"target_object_ids fast_path=objects_only"},
				MemoryPipeline: formatQueryPathMemoryPipelineLines(r.storage, objectIDs),
				Query:          []string{"query_chain skipped=objects_only"},
				Collaboration:  []string{"collaboration_chain skipped=objects_only"},
			},
		}
		applyQueryOutcomeHint(&resp, len(objectIDs))
		return resp
	}
	plan := r.planner.Build(req)
	vectorOnlyMode := vectorOnlyModeEnabled()
	searchInput := dataplane.SearchInput{
		QueryText:      req.QueryText,
		TopK:           plan.TopK,
		Namespace:      plan.Namespace,
		Constraints:    plan.Constraints,
		TimeFromUnixTS: plan.TimeFromUnixTS,
		TimeToUnixTS:   plan.TimeToUnixTS,
		IncludeGrowing: plan.IncludeGrowing,
		IncludeCold:    plan.IncludeCold,
		ObjectTypes:    plan.ObjectTypes,
		MemoryTypes:    plan.MemoryTypes,
		QueryEmbedding: req.EmbeddingVector,
	}
	result := r.nodeManager.DispatchQuery(searchInput, r.plane)
	if len(result.ObjectIDs) == 0 {
		if altQuery, ok := cjkSpacedFallbackQuery(req.QueryText); ok && altQuery != searchInput.QueryText {
			searchInput.QueryText = altQuery
			retry := r.nodeManager.DispatchQuery(searchInput, r.plane)
			if len(retry.ObjectIDs) > 0 {
				result = retry
			}
		}
	}
	result.ObjectIDs = semantic.FilterObjectIDsByTypes(result.ObjectIDs, plan.ObjectTypes)
	if queryUsesStructuredMemorySelectors(req) {
		selectorIDs := r.fetchMemoryIDsByStructuredSelectors(req)
		if req.LatestBatchOnly {
			result.ObjectIDs = selectorIDs
		} else {
			result.ObjectIDs = filterObjectIDsByStructuredSelectors(r.storage.Objects(), result.ObjectIDs, req)
			result.ObjectIDs = appendMissing(result.ObjectIDs, selectorIDs)
		}
	}
	result.ObjectIDs = appendMissing(result.ObjectIDs, r.fetchTargetObjectIDs(req))
	result.ObjectIDs = filterObjectIDsExcludingInactiveMemories(r.storage.Objects(), result.ObjectIDs, result.ColdObjectIDs)
	retrievalVisibleIDs, _ := r.filterObjectIDsByAccess(req, result.ObjectIDs, readWatermarkLSN)
	retrievalHitCount := len(retrievalVisibleIDs)

	// ── Canonical-object supplemental retrieval ──────────────────────────────
	// State, Artifact, and Event objects are stored directly in ObjectStore, not
	// in the retrieval plane. Supplement them only for an explicit object_types
	// request. Planner defaults must not turn an ordinary semantic query into an
	// unranked scan of every checkpoint State or Artifact in the scope.
	canonicalAddCount := 0
	if !vectorOnlyMode && len(req.TargetObjectIDs) == 0 && len(req.ObjectTypes) > 0 {
		canonicalIDs := r.fetchCanonicalObjects(plan.ObjectTypes, req.AgentID, req.SessionID, plan.Namespace)
		canonicalAddCount = len(canonicalIDs)
		result.ObjectIDs = append(result.ObjectIDs, canonicalIDs...)
	}
	var accessDecisions []schemas.AccessDecision
	result.ObjectIDs, accessDecisions = r.filterObjectIDsByAccess(req, result.ObjectIDs, readWatermarkLSN)

	if vectorOnlyMode {
		resp := schemas.QueryResponse{
			Objects:          result.ObjectIDs,
			AccessDecisions:  accessDecisions,
			ReadWatermarkLSN: readWatermarkLSN,
			Retrieval: &schemas.RetrievalSummary{
				Tier:               result.Tier,
				ColdSearchMode:     result.ColdSearchMode,
				ColdCandidateCount: result.ColdCandidateCount,
				ColdTierRequested:  result.ColdTierRequested,
				ColdUsedFallback:   result.ColdUsedFallback,
				RetrievalHits:      retrievalHitCount,
				CanonicalAdds:      0,
			},
			ChainTraces: schemas.ChainTraceSlots{
				Main:           append(formatQueryPathMainChainLines(req, result), "vector_only_mode=true"),
				MemoryPipeline: formatQueryPathMemoryPipelineLines(r.storage, result.ObjectIDs),
				Query:          []string{"query_chain skipped=vector_only_mode"},
				Collaboration:  []string{"collaboration_chain skipped=vector_only_mode"},
			},
		}
		applyQueryOutcomeHint(&resp, retrievalHitCount)
		return resp
	}

	if req.ResponseMode == schemas.ResponseModeObjectsOnly {
		resp := schemas.QueryResponse{
			Objects:          result.ObjectIDs,
			AccessDecisions:  accessDecisions,
			ReadWatermarkLSN: readWatermarkLSN,
			Retrieval: &schemas.RetrievalSummary{
				Tier:               result.Tier,
				ColdSearchMode:     result.ColdSearchMode,
				ColdCandidateCount: result.ColdCandidateCount,
				ColdTierRequested:  result.ColdTierRequested,
				ColdUsedFallback:   result.ColdUsedFallback,
				RetrievalHits:      retrievalHitCount,
				CanonicalAdds:      canonicalAddCount,
			},
			ChainTraces: schemas.ChainTraceSlots{
				Main:           formatQueryPathMainChainLines(req, result),
				MemoryPipeline: formatQueryPathMemoryPipelineLines(r.storage, result.ObjectIDs),
				Query:          []string{"query_chain skipped=objects_only"},
				Collaboration:  []string{"collaboration_chain skipped=objects_only"},
			},
		}
		applyQueryOutcomeHint(&resp, retrievalHitCount)
		return resp
	}

	var filters []string
	if !r.MinimalMode {
		filters = r.policy.ApplyQueryFilters(req)
	}
	resp := r.assembler.Build(searchInput, result, filters)
	resp.AccessDecisions = accessDecisions
	resp.ReadWatermarkLSN = readWatermarkLSN
	resp.Retrieval = &schemas.RetrievalSummary{
		Tier:               result.Tier,
		ColdSearchMode:     result.ColdSearchMode,
		ColdCandidateCount: result.ColdCandidateCount,
		ColdTierRequested:  result.ColdTierRequested,
		ColdUsedFallback:   result.ColdUsedFallback,
		RetrievalHits:      retrievalHitCount,
		CanonicalAdds:      canonicalAddCount,
	}
	applyQueryOutcomeHint(&resp, retrievalHitCount)

	resp.ChainTraces.Main = formatQueryPathMainChainLines(req, result)
	resp.ChainTraces.MemoryPipeline = formatQueryPathMemoryPipelineLines(r.storage, result.ObjectIDs)

	// ── Post-retrieval reasoning via QueryChain ───────────────────────────────
	// QueryChain handles:
	//   1. Pre-fetching Memory objects as GraphNodes for node population.
	//   2. Pre-fetching BulkEdges for edge pre-population.
	//   3. Multi-hop BFS proof trace via ProofTraceWorker.
	//   4. Subgraph expansion via SubgraphExecutorWorker.
	//   5. Merging subgraph edges with the assembler's edges (deduplicated).
	//
	// In VECTOR-ONLY MODE: skip QueryChain (graph expansion, proof trace, provenance).
	if r.VectorOnlyMode {
		resp.ChainTraces.Query = []string{"query_chain skipped=vector_only_mode"}
	} else if len(result.ObjectIDs) > 0 {
		chainOut, chainResult := r.queryChain.Run(chain.QueryChainInput{
			ObjectIDs:   result.ObjectIDs,
			MaxDepth:    0, // default cap of 8
			ObjectStore: r.storage.Objects(),
			EdgeStore:   r.storage.Edges(),
		})

		if len(chainOut.ProofTrace) > 0 {
			resp.ProofTrace = append(resp.ProofTrace, chainOut.ProofTrace...)
		}

		if len(chainOut.Subgraph.Nodes) > 0 {
			resp.Nodes = chainOut.Subgraph.Nodes
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
		resp.ChainTraces.Query = formatQueryChainTraceLines(chainResult, chainOut)
	} else {
		resp.ChainTraces.Query = []string{"query_chain skipped=no_seed_object_ids"}
	}

	resp.ChainTraces.Collaboration = formatQueryPathCollaborationLines(resp.Edges)

	// In VECTOR-ONLY MODE or MINIMAL MODE: skip provenance attachment
	if !r.VectorOnlyMode && !r.MinimalMode {
		resp = r.attachEmbeddingProvenance(resp, req, result.ObjectIDs)
	}
	r.filterResponseEvidenceByAccess(req, readWatermarkLSN, &resp)

	// Record evidence-supported rate (3-MT5)
	evidenceSupported := len(resp.ProofTrace) > 0 || len(resp.Nodes) > 0
	metrics.Global().RecordEvidenceSupported(evidenceSupported)
	if req.SessionID != "" {
		metrics.Global().Session(req.SessionID).RecordQuery(evidenceSupported)
	}

	// Cross-agent contamination detection (4-M1).
	// When governance is active, check returned memories for cross-scope leakage.
	if req.AgentID != "" && !r.GovernanceDisabled {
		r.detectContamination(req.AgentID, result.ObjectIDs)
	}

	return resp
}

// detectContamination counts memory IDs that belong to a different agent and
// are not covered by any share contract.  Each such ID increments the global
// contamination counter (4-M1).
func (r *Runtime) detectContamination(requesterAgentID string, objectIDs []string) {
	principal := semantic.AccessPrincipal{AgentID: requesterAgentID}
	contracts := r.storage.Contracts().ListContracts()
	for _, id := range objectIDs {
		mem, ok := r.storage.Objects().GetMemory(id)
		if !ok {
			continue
		}
		if mem.AgentID == "" || mem.AgentID == requesterAgentID {
			continue
		}
		record := memoryAccessRecord(mem)
		principal.TenantID = record.access.TenantID
		principal.WorkspaceID = record.access.WorkspaceID
		principal.TeamID = record.access.TeamID
		principal.SessionID = record.access.SessionID
		_, allowed := r.policy.EvaluateAccess(
			id,
			record.ownerAgentID,
			record.access,
			principal,
			contracts,
			record.mutationLSN,
			math.MaxInt64,
		)
		if !allowed {
			metrics.Global().RecordContaminationAttempt()
		}
	}
}

func vectorOnlyModeEnabled() bool {
	return envBool("PLASMOD_VECTOR_ONLY_MODE")
}

func visibilityOnlyModeEnabled() bool {
	return envBool("PLASMOD_VISIBILITY_ONLY")
}

func envBool(key string) bool {
	raw := strings.TrimSpace(os.Getenv(key))
	switch strings.ToLower(raw) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// applyQueryOutcomeHint sets query_status / query_hint so clients can tell "no dataset"
// from an empty or misleading object list (e.g. only unrelated artifacts).
func applyQueryOutcomeHint(resp *schemas.QueryResponse, retrievalHits int) {
	if retrievalHits > 0 {
		resp.QueryStatus = "ok"
		return
	}
	if len(resp.Objects) == 0 {
		resp.QueryStatus = "no_retrieval_hits"
		resp.QueryHint = "检索主路径未命中任何对象。若期望某数据集：请确认 workspace/query_scope 与导入时一致、数据已写入、未被软删除；也可尝试放宽 TopK、检查 object_types，或开启 include_cold。"
		return
	}
	resp.QueryStatus = "no_retrieval_hits_supplemented"
	resp.QueryHint = "语义/向量检索未命中；当前 objects 可能仅来自会话下的 event/state/artifact 等补充列表，与查询文本不一定相关。"
}

// filterObjectIDsExcludingInactiveMemories drops memory IDs whose canonical Memory
// exists in ObjectStore with IsActive=false (soft-deleted dataset rows).
// coldIDs is a set of IDs that originated from the cold tier; those are exempted
// because archived memories may be soft-deleted in warm but must still be surfaced
// when include_cold=true is requested.
func filterObjectIDsExcludingInactiveMemories(os storage.ObjectStore, ids []string, coldIDs []string) []string {
	if os == nil || len(ids) == 0 {
		return ids
	}
	coldSet := make(map[string]bool, len(coldIDs))
	for _, id := range coldIDs {
		coldSet[id] = true
	}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if coldSet[id] {
			out = append(out, id)
			continue
		}
		if m, ok := os.GetMemory(id); ok && !m.IsActive {
			continue
		}
		out = append(out, id)
	}
	return out
}

func queryUsesStructuredMemorySelectors(req schemas.QueryRequest) bool {
	return strings.TrimSpace(req.DatasetName) != "" ||
		strings.TrimSpace(req.SourceFileName) != "" ||
		strings.TrimSpace(req.ImportBatchID) != "" ||
		req.LatestBatchOnly
}

func appendMissing(base []string, extras []string) []string {
	if len(extras) == 0 {
		return base
	}
	seen := make(map[string]struct{}, len(base))
	for _, id := range base {
		seen[id] = struct{}{}
	}
	for _, id := range extras {
		if _, ok := seen[id]; ok {
			continue
		}
		base = append(base, id)
		seen[id] = struct{}{}
	}
	return base
}

func filterObjectIDsByStructuredSelectors(os storage.ObjectStore, ids []string, req schemas.QueryRequest) []string {
	if os == nil || len(ids) == 0 || !queryUsesStructuredMemorySelectors(req) {
		return ids
	}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		mem, ok := os.GetMemory(id)
		if !ok {
			continue
		}
		if !memoryMatchesStructuredSelectorsBase(mem, req) {
			continue
		}
		if req.ImportBatchID != "" && mem.ImportBatchID != strings.TrimSpace(req.ImportBatchID) {
			continue
		}
		out = append(out, id)
	}
	return out
}

func (r *Runtime) fetchMemoryIDsByStructuredSelectors(req schemas.QueryRequest) []string {
	if r.storage == nil || r.storage.Objects() == nil {
		return nil
	}
	all := r.storage.Objects().ListMemories("", "")
	if len(all) == 0 {
		return nil
	}
	matched := make([]schemas.Memory, 0, len(all))
	for _, mem := range all {
		if !memoryMatchesStructuredSelectorsBase(mem, req) {
			continue
		}
		matched = append(matched, mem)
	}
	if len(matched) == 0 {
		return nil
	}
	if batchID := strings.TrimSpace(req.ImportBatchID); batchID != "" {
		filtered := make([]schemas.Memory, 0, len(matched))
		for _, mem := range matched {
			if mem.ImportBatchID == batchID {
				filtered = append(filtered, mem)
			}
		}
		matched = filtered
		if len(matched) == 0 {
			return nil
		}
	}
	if req.LatestBatchOnly {
		latestBatchID := resolveLatestImportBatchID(matched)
		if latestBatchID != "" {
			filtered := make([]schemas.Memory, 0, len(matched))
			for _, mem := range matched {
				if mem.ImportBatchID == latestBatchID {
					filtered = append(filtered, mem)
				}
			}
			matched = filtered
		}
		if len(matched) == 0 {
			return nil
		}
	}
	sort.SliceStable(matched, func(i, j int) bool {
		ti, _ := time.Parse(time.RFC3339, matched[i].ValidFrom)
		tj, _ := time.Parse(time.RFC3339, matched[j].ValidFrom)
		return ti.After(tj)
	})
	limit := req.TopK
	if limit <= 0 || limit > len(matched) {
		limit = len(matched)
	}
	ids := make([]string, 0, limit)
	for i := 0; i < limit; i++ {
		ids = append(ids, matched[i].MemoryID)
	}
	return ids
}

func (r *Runtime) fetchTargetObjectIDs(req schemas.QueryRequest) []string {
	if r.storage == nil || len(req.TargetObjectIDs) == 0 {
		return nil
	}
	objects := r.storage.Objects()
	edges := r.storage.Edges()
	out := make([]string, 0, len(req.TargetObjectIDs))
	for _, raw := range req.TargetObjectIDs {
		id := strings.TrimSpace(raw)
		if id == "" {
			continue
		}
		if objects != nil {
			if mem, ok := objects.GetMemory(id); ok && memoryMatchesStructuredSelectorsBase(mem, req) {
				out = append(out, id)
				continue
			}
			if st, ok := objects.GetState(id); ok && stateMatchesQuerySelectors(st, req) {
				out = append(out, id)
				continue
			}
			if art, ok := objects.GetArtifact(id); ok && artifactMatchesQuerySelectors(art, req) {
				out = append(out, id)
				continue
			}
			if ev, ok := objects.GetEvent(id); ok && eventMatchesQuerySelectors(ev.NormalizeDynamicEventV04(), req) {
				out = append(out, id)
				continue
			}
		}
		if edges != nil {
			if edge, ok := edges.GetEdge(id); ok && edgeMatchesQuerySelectors(edge, req) {
				out = append(out, id)
			}
		}
	}
	return out
}

func memoryMatchesStructuredSelectorsBase(mem schemas.Memory, req schemas.QueryRequest) bool {
	workspaceID := strings.TrimSpace(req.WorkspaceID)
	if workspaceID != "" && mem.Scope != workspaceID {
		return false
	}
	agentID := strings.TrimSpace(req.AgentID)
	if agentID != "" && mem.AgentID != agentID {
		return false
	}
	sessionID := strings.TrimSpace(req.SessionID)
	if sessionID != "" && mem.SessionID != sessionID {
		return false
	}
	datasetName := strings.TrimSpace(req.DatasetName)
	if datasetName != "" && mem.DatasetName != datasetName {
		return false
	}
	sourceFile := strings.TrimSpace(req.SourceFileName)
	if sourceFile != "" && mem.SourceFileName != sourceFile {
		return false
	}
	return true
}

func stateMatchesQuerySelectors(st schemas.State, req schemas.QueryRequest) bool {
	agentID := strings.TrimSpace(req.AgentID)
	if agentID != "" && st.AgentID != agentID {
		return false
	}
	sessionID := strings.TrimSpace(req.SessionID)
	if sessionID != "" && st.SessionID != sessionID {
		return false
	}
	return true
}

func artifactMatchesQuerySelectors(art schemas.Artifact, req schemas.QueryRequest) bool {
	agentID := strings.TrimSpace(req.AgentID)
	if agentID != "" && art.OwnerAgentID != "" && art.OwnerAgentID != agentID {
		return false
	}
	sessionID := strings.TrimSpace(req.SessionID)
	if sessionID != "" && art.SessionID != sessionID {
		return false
	}
	return true
}

func eventMatchesQuerySelectors(ev schemas.Event, req schemas.QueryRequest) bool {
	workspaceID := strings.TrimSpace(req.WorkspaceID)
	if workspaceID != "" && ev.Identity.WorkspaceID != "" && ev.Identity.WorkspaceID != workspaceID {
		return false
	}
	agentID := strings.TrimSpace(req.AgentID)
	if agentID != "" && ev.Actor.AgentID != agentID {
		return false
	}
	sessionID := strings.TrimSpace(req.SessionID)
	if sessionID != "" && ev.Actor.SessionID != sessionID {
		return false
	}
	return true
}

func edgeMatchesQuerySelectors(edge schemas.Edge, req schemas.QueryRequest) bool {
	return true
}

func resolveLatestImportBatchID(mems []schemas.Memory) string {
	latestBatchID := ""
	latestVersion := int64(-1)
	latestTS := time.Time{}
	for _, mem := range mems {
		if strings.TrimSpace(mem.ImportBatchID) == "" {
			continue
		}
		if mem.Version > latestVersion {
			latestBatchID = mem.ImportBatchID
			latestVersion = mem.Version
			ts, _ := time.Parse(time.RFC3339, mem.ValidFrom)
			latestTS = ts
			continue
		}
		if mem.Version < latestVersion {
			continue
		}
		ts, err := time.Parse(time.RFC3339, mem.ValidFrom)
		if err != nil {
			continue
		}
		if latestBatchID == "" || ts.After(latestTS) {
			latestBatchID = mem.ImportBatchID
			latestTS = ts
		}
	}
	return latestBatchID
}

// fetchCanonicalObjects retrieves State and Artifact object IDs from the canonical
// ObjectStore for the given agent/session/namespace.  These types bypass the
// retrieval plane and are stored directly in ObjectStore by the materialization
// workers, so they must be fetched explicitly to appear in query responses.
func (r *Runtime) fetchCanonicalObjects(objectTypes []string, agentID, sessionID, namespace string) []string {
	var ids []string
	for _, t := range objectTypes {
		switch t {
		case "event":
			if r.storage != nil {
				for _, e := range r.storage.Objects().ListEvents(agentID, sessionID) {
					ev := e.NormalizeDynamicEventV04()
					ids = append(ids, ev.Identity.EventID)
				}
			}

		case "state", "agent_state":
			if r.storage != nil {
				for _, s := range r.storage.Objects().ListStates(agentID, sessionID) {
					ids = append(ids, s.StateID)
				}
			}
		case "artifact":
			if r.storage != nil {
				for _, a := range r.storage.Objects().ListArtifacts(sessionID) {
					ids = append(ids, a.ArtifactID)
				}
			}
		case "edge", "relation":
			if r.storage != nil && r.storage.Edges() != nil {
				for _, e := range r.storage.Edges().ListEdges() {
					ids = append(ids, e.EdgeID)
				}
			}
		}
	}
	return ids
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

// ─── Algorithm dispatch (used by Agent SDK internal endpoints) ─────────────────

// DispatchAlgorithm forwards an operation to all registered AlgorithmDispatchWorkers.
// Supported operations: "ingest" | "decay" | "recall" | "compress" | "summarize" | "update".
// Returns an empty output when no algorithm workers are registered.
func (r *Runtime) DispatchAlgorithm(
	operation string,
	memoryIDs []string,
	query, nowTS, agentID, sessionID string,
	signals map[string]float64,
) schemas.AlgorithmDispatchOutput {
	if r.nodeManager == nil {
		return schemas.AlgorithmDispatchOutput{Operation: operation}
	}
	return r.nodeManager.DispatchAlgorithmDispatch(operation, memoryIDs, query, nowTS, agentID, sessionID, signals)
}

// DispatchRecall combines query retrieval with algorithm-level Recall scoring.
// It builds a QueryRequest from the parameters, executes it, then passes the
// top-ranked candidate IDs to AlgorithmDispatchWorker.Recall and returns a MemoryView.
func (r *Runtime) DispatchRecall(
	query, scope string,
	topK int,
	agentID, sessionID, tenantID, workspaceID string,
) schemas.MemoryView {
	now := time.Now().UTC()
	req := schemas.QueryRequest{
		QueryText:   query,
		QueryScope:  scope,
		SessionID:   sessionID,
		AgentID:     agentID,
		TenantID:    tenantID,
		WorkspaceID: workspaceID,
		TopK:        topK,
		TimeWindow: schemas.TimeWindow{
			From: "1970-01-01T00:00:00Z",
			To:   now.Format(time.RFC3339),
		},
		ObjectTypes:  []string{"memory"},
		MemoryTypes:  []string{"semantic", "episodic", "procedural"},
		ResponseMode: schemas.ResponseModeStructuredEvidence,
	}

	resp := r.ExecuteQuery(req)
	if len(resp.Objects) == 0 {
		if altQuery, ok := cjkSpacedFallbackQuery(query); ok {
			req.QueryText = altQuery
			resp = r.ExecuteQuery(req)
		}
	}
	visibleRefs := resp.Objects
	if len(visibleRefs) == 0 {
		return schemas.MemoryView{
			RequestID:     fmt.Sprintf("recall_%d", now.UnixNano()),
			RequesterID:   agentID,
			AgentID:       agentID,
			ResolvedScope: scope,
			BackendMode:   r.MemoryBackendMode(),
		}
	}

	// Forward the top candidates to algorithm-level Recall for scored re-ranking.
	algoOut := schemas.AlgorithmDispatchOutput{}
	if r.nodeManager != nil {
		algoOut = r.nodeManager.DispatchAlgorithmDispatch(
			"recall", visibleRefs, query,
			now.Format(time.RFC3339), agentID, sessionID, nil,
		)
	}

	// Use algorithm-ordered refs if available, otherwise fall back to search order.
	orderedRefs := visibleRefs
	if len(algoOut.ScoredRefs) > 0 {
		orderedRefs = algoOut.ScoredRefs
	}
	var algoNotes []string
	if len(algoOut.ScoredRefs) > 0 {
		algoNotes = []string{fmt.Sprintf("algorithm_scored:%d", len(algoOut.ScoredRefs))}
	} else {
		algoNotes = []string{"search_fallback:no_algo_worker"}
	}
	activeAlgo := strings.TrimSpace(os.Getenv("PLASMOD_ACTIVE_ALGORITHM"))
	if activeAlgo == "" {
		activeAlgo = "memorybank"
	}
	algoNotes = append(algoNotes, "algorithm_profile="+activeAlgo)
	// Convert ProofStep slice to string slice for MemoryView.ConstructionTrace
	proofStrs := make([]string, len(resp.ProofTrace))
	for i, step := range resp.ProofTrace {
		proofStrs[i] = fmt.Sprintf("%s:%s", step.StepType, step.SourceID)
	}
	// Collect full Memory payloads for the final ordered refs.
	payloads := make([]schemas.Memory, 0, len(orderedRefs))
	for _, id := range orderedRefs {
		if mem, ok := r.storage.Objects().GetMemory(id); ok {
			payloads = append(payloads, mem)
		}
	}

	return schemas.MemoryView{
		RequestID:         fmt.Sprintf("recall_%d", now.UnixNano()),
		RequesterID:       agentID,
		AgentID:           agentID,
		ResolvedScope:     scope,
		VisibleMemoryRefs: orderedRefs,
		Payloads:          payloads,
		BackendMode:       r.MemoryBackendMode(),
		RecallSources:     []string{"local"},
		ProvenanceNotes:   []string{fmt.Sprintf("search_rank:%d_algo_rank:%d", len(visibleRefs), len(orderedRefs))},
		AlgorithmNotes:    algoNotes,
		ConstructionTrace: proofStrs,
	}
}

func cjkSpacedFallbackQuery(query string) (string, bool) {
	q := strings.TrimSpace(query)
	if q == "" {
		return "", false
	}
	hasCJK := false
	for _, r := range q {
		if unicode.Is(unicode.Han, r) {
			hasCJK = true
			break
		}
	}
	if !hasCJK {
		return "", false
	}
	parts := make([]string, 0, len([]rune(q)))
	for _, r := range q {
		if unicode.IsSpace(r) {
			continue
		}
		parts = append(parts, string(r))
	}
	if len(parts) == 0 {
		return "", false
	}
	alt := strings.Join(parts, " ")
	if alt == q {
		return "", false
	}
	return alt, true
}

func (r *Runtime) MemoryBackendMode() string {
	if r == nil || r.memoryBackend == nil {
		return MemoryBackendLocalOnly
	}
	return r.memoryBackend.Mode()
}

func (r *Runtime) MemoryBackendHealth(ctx context.Context) map[string]any {
	if r == nil || r.memoryBackend == nil {
		return map[string]any{"mode": MemoryBackendLocalOnly, "status": "ok"}
	}
	out := r.memoryBackend.Health(ctx)
	return out
}

func (r *Runtime) SetMemoryBackendMode(mode string) bool {
	if r == nil || r.memoryBackend == nil {
		return mode == MemoryBackendLocalOnly
	}
	return r.memoryBackend.SetMode(mode)
}

func (r *Runtime) EnqueueMemoryDelete(memoryID string, hard bool, reason string) bool {
	return false
}

func (r *Runtime) MemoryDeleteOutboxStats() map[string]any {
	return map[string]any{"enabled": false}
}

// DispatchShare creates an event-derived shared canonical Memory. The source
// remains the owner and the target receives an explicit read grant.
func (r *Runtime) DispatchShare(fromAgentID, toAgentID, memoryID string) (string, error) {
	return r.DispatchShareWithContract(fromAgentID, toAgentID, memoryID, "")
}

// DispatchShareWithContract validates an optional ShareContract before the
// derived object enters the normal WAL/materialization/visibility pipeline.
func (r *Runtime) DispatchShareWithContract(fromAgentID, toAgentID, memoryID, contractID string) (string, error) {
	fromAgentID = strings.TrimSpace(fromAgentID)
	toAgentID = strings.TrimSpace(toAgentID)
	memoryID = strings.TrimSpace(memoryID)
	if fromAgentID == "" || toAgentID == "" || memoryID == "" {
		return "", fmt.Errorf("%w: memory_id, from_agent_id, and to_agent_id are required", ErrShareInvalid)
	}
	mem, ok := r.peekCanonicalMemory(memoryID)
	if !ok {
		return "", fmt.Errorf("%w: memory %s", ErrShareNotFound, memoryID)
	}
	sourceRecord := memoryAccessRecord(mem)
	ownerAgentID := sourceRecord.ownerAgentID
	if ownerAgentID != "" && ownerAgentID != fromAgentID {
		return "", fmt.Errorf("%w: agent %s does not own memory %s", ErrShareForbidden, fromAgentID, memoryID)
	}
	if fromAgentID == toAgentID {
		return "", nil // no-op
	}
	contractID = strings.TrimSpace(contractID)
	if contractID != "" {
		contract, found := r.storage.Contracts().GetContract(contractID)
		if !found {
			return "", fmt.Errorf("%w: share contract %s", ErrShareNotFound, contractID)
		}
		if !shareContractMatchesSourceScope(contract, sourceRecord.access) {
			return "", fmt.Errorf("%w: share contract %s does not match memory scope", ErrShareForbidden, contractID)
		}
		source := r.accessPrincipal(schemas.QueryRequest{
			TenantID: sourceRecord.access.TenantID, WorkspaceID: sourceRecord.access.WorkspaceID,
			TeamID: sourceRecord.access.TeamID, SessionID: sourceRecord.access.SessionID,
			RequesterAgentID: fromAgentID,
		})
		target := r.accessPrincipal(schemas.QueryRequest{
			TenantID: sourceRecord.access.TenantID, WorkspaceID: sourceRecord.access.WorkspaceID,
			TeamID: sourceRecord.access.TeamID, SessionID: sourceRecord.access.SessionID,
			RequesterAgentID: toAgentID,
		})
		if !r.policy.IsShareContractAllowed("derive", contract, source) {
			return "", fmt.Errorf("%w: share contract %s denies derive for %s", ErrShareForbidden, contractID, fromAgentID)
		}
		if !r.policy.IsShareContractAllowed("read", contract, target) {
			return "", fmt.Errorf("%w: share contract %s denies read for %s", ErrShareForbidden, contractID, toAgentID)
		}
	}
	sharedID := schemas.IDPrefixShared + memoryID + "_to_" + toAgentID
	importance := mem.Importance
	confidence := mem.Confidence
	event := schemas.Event{
		Identity: schemas.EventIdentity{
			EventID:     fmt.Sprintf("share_%s_to_%s_%d", memoryID, toAgentID, time.Now().UnixNano()),
			TenantID:    sourceRecord.access.TenantID,
			WorkspaceID: sourceRecord.access.WorkspaceID,
		},
		Actor: schemas.EventActor{AgentID: fromAgentID, TeamID: sourceRecord.access.TeamID, SessionID: sourceRecord.access.SessionID},
		EventInfo: schemas.EventDescriptor{
			EventType: string(schemas.EventTypeMemoryWriteRequested), Importance: &importance, Confidence: &confidence,
		},
		Object: schemas.EventObject{ObjectID: sharedID, ObjectType: string(schemas.ObjectTypeMemory), ObjectSubtype: "shared"},
		Causality: schemas.EventCausality{
			SourceObjectID: memoryID, TargetObjectID: sharedID, EdgeKind: string(schemas.EdgeTypeDerivedFrom),
		},
		Access: schemas.EventAccess{
			Consistency: string(schemas.AccessConsistencyStrict), Visibility: string(schemas.MemoryScopeRestrictedShared),
			VisibleToAgents: []string{toAgentID}, PolicyTags: append([]string(nil), sourceRecord.access.PolicyTags...), ShareContractID: contractID,
		},
		Payload: map[string]any{
			"text": mem.Content, "memory_type": mem.MemoryType,
			"shared_from_memory_id": memoryID, "shared_to_agent_id": toAgentID,
		},
	}
	if _, err := r.SubmitIngest(event); err != nil {
		return "", err
	}
	return sharedID, nil
}

func shareContractMatchesSourceScope(contract schemas.ShareContract, access schemas.CanonicalAccess) bool {
	if contract.TenantID != "" && contract.TenantID != access.TenantID {
		return false
	}
	if contract.WorkspaceID != "" && contract.WorkspaceID != access.WorkspaceID {
		return false
	}
	if contract.Scope == "" {
		return true
	}
	return contract.Scope == access.WorkspaceID ||
		contract.Scope == access.TeamID ||
		contract.Scope == access.SessionID
}

// DispatchConflictResolve resolves a memory conflict and returns the winner ID.
func (r *Runtime) DispatchConflictResolve(leftID, rightID string) string {
	if r.nodeManager != nil {
		return r.nodeManager.DispatchConflictMergeWithWinner(leftID, rightID, "memory")
	}
	return leftID
}

// Manager returns the underlying nodes.Manager. It may be nil.
func (r *Runtime) Manager() *nodes.Manager {
	return r.nodeManager
}

// formatQueryPathMainChainLines summarizes retrieval context on the read path.
// MainChain itself runs on ingest only; we do not replay it during query.
func formatQueryPathMainChainLines(req schemas.QueryRequest, result dataplane.SearchOutput) []string {
	return []string{
		"phase=query_path",
		"main_chain not_reexecuted=runs_on_ingest",
		fmt.Sprintf("retrieval_tier=%s", result.Tier),
		fmt.Sprintf("retrieved_object_ids=%d", len(result.ObjectIDs)),
		fmt.Sprintf("include_cold_requested=%t", req.IncludeCold),
	}
}

// formatQueryPathMemoryPipelineLines summarizes memory seeds from retrieval.
// MemoryPipelineChain runs on ingest / subscriber; query only reports store-backed stats.
func formatQueryPathMemoryPipelineLines(store storage.RuntimeStorage, objectIDs []string) []string {
	lines := []string{
		"phase=query_path",
		"memory_pipeline_chain not_reexecuted=runs_on_ingest_and_subscriber",
	}
	if store == nil {
		lines = append(lines, "retrieved_memory_seeds=0", "object_store=nil")
		return lines
	}
	nMem := 0
	maxLevel := 0
	nChecked := 0
	const maxLevelLookups = 64
	objs := store.Objects()
	for _, id := range objectIDs {
		if !strings.HasPrefix(id, schemas.IDPrefixMemory) {
			continue
		}
		nMem++
		if nChecked >= maxLevelLookups {
			continue
		}
		nChecked++
		if m, ok := objs.GetMemory(id); ok && m.Level > maxLevel {
			maxLevel = m.Level
		}
	}
	lines = append(lines, fmt.Sprintf("retrieved_memory_seeds=%d", nMem))
	if nMem > 0 && nChecked > 0 {
		lines = append(lines, fmt.Sprintf("sample_max_memory_level=%d", maxLevel))
	}
	if nMem > nChecked {
		lines = append(lines, fmt.Sprintf("memory_level_stats_capped=%d", maxLevelLookups))
	}
	return lines
}

// formatQueryPathCollaborationLines summarizes collaboration-related edges present
// in the assembled response (including subgraph merge). CollaborationChain runs on ingest.
func formatQueryPathCollaborationLines(edges []schemas.Edge) []string {
	lines := []string{
		"phase=query_path",
		"collaboration_chain not_reexecuted=runs_on_ingest_and_conflict_merge",
	}
	var nConflict, nSession, nAgent int
	for _, e := range edges {
		switch e.EdgeType {
		case string(schemas.EdgeTypeConflictResolved):
			nConflict++
		case string(schemas.EdgeTypeBelongsToSession):
			nSession++
		case string(schemas.EdgeTypeOwnedByAgent):
			nAgent++
		}
	}
	lines = append(lines,
		fmt.Sprintf("edges_in_response_total=%d", len(edges)),
		fmt.Sprintf("edges_conflict_resolved=%d", nConflict),
		fmt.Sprintf("edges_belongs_to_session=%d", nSession),
		fmt.Sprintf("edges_owned_by_agent=%d", nAgent),
	)
	return lines
}

// formatQueryChainTraceLines turns QueryChain results into API-facing trace lines.
func formatQueryChainTraceLines(res chain.ChainResult, out chain.QueryChainOutput) []string {
	lines := []string{
		fmt.Sprintf("chain_name=%s ok=%t", res.ChainName, res.OK),
	}
	if res.Error != "" {
		lines = append(lines, "error="+res.Error)
	}
	if len(res.Meta) > 0 {
		keys := make([]string, 0, len(res.Meta))
		for k := range res.Meta {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			lines = append(lines, fmt.Sprintf("meta.%s=%v", k, res.Meta[k]))
		}
	}
	lines = append(lines,
		fmt.Sprintf("query_proof_steps=%d", len(out.ProofTrace)),
		fmt.Sprintf("subgraph_nodes=%d", len(out.Subgraph.Nodes)),
		fmt.Sprintf("subgraph_edges=%d", len(out.Subgraph.Edges)),
		fmt.Sprintf("merged_edges=%d", len(out.MergedEdges)),
	)
	return lines
}

func currentEmbeddingDim() int {
	if dimStr := strings.TrimSpace(os.Getenv("PLASMOD_EMBEDDER_DIM")); dimStr != "" {
		if dim, err := strconv.Atoi(dimStr); err == nil && dim > 0 {
			return dim
		}
	}
	embedder := strings.TrimSpace(os.Getenv("PLASMOD_EMBEDDER"))
	if embedder == "" || embedder == "tfidf" {
		return dataplane.DefaultEmbeddingDim
	}
	return 0
}

func (r *Runtime) attachEmbeddingProvenance(
	resp schemas.QueryResponse,
	req schemas.QueryRequest,
	currentIDs []string,
) schemas.QueryResponse {
	spec := r.EmbeddingSpec()
	if !spec.Valid() {
		spec = storage.ResolveEmbeddingSpec(nil, "", currentEmbeddingDim())
	}
	resp.Provenance = append(resp.Provenance,
		fmt.Sprintf("embedding_runtime_family=%s", spec.Family),
		fmt.Sprintf("embedding_runtime_dim=%d", spec.Dim),
	)
	candidateLists := [][]string{}
	if len(currentIDs) > 0 {
		candidateLists = append(candidateLists, currentIDs)
	}
	if len(candidateLists) > 0 {
		topK := req.TopK
		if len(req.TargetObjectIDs) > topK {
			topK = len(req.TargetObjectIDs)
		}
		fused := rrfFuseStringLists(candidateLists, 60, topK)
		if len(fused) > 0 {
			resp.Objects = fused
		}
	}
	resp.Provenance = append(resp.Provenance,
		"cross_dim_fusion=rrf_result_layer",
		fmt.Sprintf("cross_dim_candidates=%d", len(candidateLists)),
	)
	return resp
}

func rrfFuseStringLists(lists [][]string, k int, topK int) []string {
	if k <= 0 {
		k = 60
	}
	scores := map[string]float64{}
	order := make([]string, 0)
	for _, ids := range lists {
		for rank, id := range ids {
			if _, ok := scores[id]; !ok {
				order = append(order, id)
			}
			scores[id] += 1.0 / float64(k+rank+1)
		}
	}
	sort.SliceStable(order, func(i, j int) bool {
		return scores[order[i]] > scores[order[j]]
	})
	if topK > 0 && len(order) > topK {
		return order[:topK]
	}
	return order
}

func validateEmbeddingIngestPayload(ev schemas.Event) error {
	if ev.Payload == nil {
		return nil
	}
	runtimeDim := currentEmbeddingDim()
	if runtimeDim <= 0 {
		return nil
	}

	// Only enforce runtime-dim constraints when an explicit vector/embedding is provided.
	// Metadata-only fields like payload.dim from dataset import are informational and can
	// differ across datasets within one runtime.
	vecLen, hasVector := payloadVectorLen(ev.Payload)
	if !hasVector {
		return nil
	}
	if payloadDim, ok := payloadEmbeddingDim(ev.Payload); ok && payloadDim != runtimeDim {
		return fmt.Errorf("embedding_dim_mismatch payload=%d runtime=%d", payloadDim, runtimeDim)
	}
	if vecLen != runtimeDim {
		return fmt.Errorf("embedding_vector_len_mismatch payload=%d runtime=%d", vecLen, runtimeDim)
	}
	return nil
}

func payloadEmbeddingDim(payload map[string]any) (int, bool) {
	raw, ok := payload["embedding_dim"]
	if !ok {
		raw, ok = payload["dim"]
		if !ok {
			return 0, false
		}
	}
	switch v := raw.(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case float64:
		return int(v), true
	case string:
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return n, true
		}
	}
	return 0, false
}

func payloadVectorLen(payload map[string]any) (int, bool) {
	raw, ok := payload["embedding"]
	if !ok {
		raw, ok = payload["vector"]
		if !ok {
			return 0, false
		}
	}
	switch v := raw.(type) {
	case []float32:
		return len(v), true
	case []float64:
		return len(v), true
	case []any:
		return len(v), true
	}
	return 0, false
}

// AdminWipeAll clears durable stores, retrieval indexes, tier caches, WAL/derivation logs,
// and evidence fragments. When bundle.Badger is set, Badger.DropAll runs first.
// S3/MinIO cold objects are not deleted; see response "cold_tier" and "note".
func (r *Runtime) AdminWipeAll(bundle *storage.RuntimeBundle, algoCfg schemas.AlgorithmConfig) (map[string]any, error) {
	if r == nil {
		return nil, errors.New("runtime is nil")
	}
	r.wipeMu.Lock()
	defer r.wipeMu.Unlock()

	r.consistencyMu.Lock()
	pauseTimeout := r.consistencyConfig.ShutdownTimeout
	r.consistencyMu.Unlock()
	if pauseTimeout <= 0 {
		pauseTimeout = 30 * time.Second
	}
	pauseCtx, cancelPause := context.WithTimeout(context.Background(), pauseTimeout)
	defer cancelPause()
	wasActive, err := r.pauseConsistencyForReset(pauseCtx)
	if err != nil {
		return nil, fmt.Errorf("pause consistency projection: %w", err)
	}
	if wasActive {
		defer r.resumeConsistency()
	}
	pausedSubscribers := r.pauseSubscribers()
	defer resumeSubscribers(pausedSubscribers)

	out := map[string]any{"status": "ok"}
	if bundle != nil && bundle.Badger != nil {
		if err := bundle.Badger.DropAll(); err != nil {
			return nil, err
		}
		out["badger_drop_all"] = true
	} else {
		out["badger_drop_all"] = false
	}

	storage.WipeMutableRuntimeState(r.storage)

	if r.tieredObjects != nil {
		out["cold_tier"] = r.tieredObjects.ClearColdIfInMemory()
	} else {
		out["cold_tier"] = "none"
	}

	if tp, ok := r.plane.(*dataplane.TieredDataPlane); ok {
		if err := tp.AdminResetRetrieval(algoCfg); err != nil {
			return nil, err
		}
		out["retrieval_plane"] = "tiered_reset"
	} else {
		out["retrieval_plane"] = "skipped_non_tiered"
	}

	out["wal"] = r.adminWipeWAL()
	resetSubscribers(pausedSubscribers)
	if err := r.resetConsistency(); err != nil {
		return nil, fmt.Errorf("reset consistency projection: %w", err)
	}
	out["consistency_projection"] = "reset"

	if r.derivationLog != nil {
		if err := r.derivationLog.Wipe(); err != nil {
			return nil, err
		}
		out["derivation_log"] = "cleared"
	}
	if r.policyDecisionLog != nil {
		r.policyDecisionLog.Wipe()
		out["policy_decision_log"] = "cleared"
	}
	if r.evCache != nil {
		r.evCache.Clear()
		out["evidence_cache"] = "cleared"
	}

	r.lastMemMu.Lock()
	r.lastMem = make(map[string]string)
	r.lastMemMu.Unlock()

	go releaseOSMemory()
	out["go_heap_release_scheduled"] = true
	out["note"] = "S3/MinIO cold objects are not deleted by this endpoint; re-ingest or use bucket tools if needed."
	return out, nil
}

func (r *Runtime) adminWipeWAL() string {
	if r == nil || r.wal == nil {
		return "none"
	}
	switch w := r.wal.(type) {
	case *eventbackbone.FileWAL:
		if err := w.Wipe(); err != nil {
			return "file_error:" + err.Error()
		}
		return "file_removed"
	case *eventbackbone.InMemoryWAL:
		w.Wipe()
		return "memory_cleared"
	default:
		return "unknown_skipped"
	}
}

// AdminReplayPreview scans WAL entries from fromLSN and returns a replay-oriented
// summary for operational validation. It does not mutate runtime state.
func (r *Runtime) AdminReplayPreview(fromLSN int64, limit int) (map[string]any, error) {
	if r == nil || r.wal == nil {
		return nil, errors.New("wal not configured")
	}
	if fromLSN < 0 {
		fromLSN = 0
	}
	entries, err := eventbackbone.ScanWAL(r.wal, fromLSN)
	if err != nil {
		return nil, fmt.Errorf("scan WAL for replay preview: %w", err)
	}
	total := len(entries)
	if total == 0 {
		return map[string]any{
			"status":               "ok",
			"from_lsn":             fromLSN,
			"latest_lsn":           r.wal.LatestLSN(),
			"scanned_entries":      0,
			"sampled_entries":      0,
			"event_type_counts":    map[string]int{},
			"sample_event_ids":     []string{},
			"first_sample_lsn":     int64(0),
			"last_sample_lsn":      int64(0),
			"replay_apply_enabled": false,
			"note":                 "preview only; no state mutation performed",
		}, nil
	}
	if limit <= 0 || limit > total {
		limit = total
	}
	sampled := entries[:limit]
	counts := make(map[string]int)
	sampleIDs := make([]string, 0, len(sampled))
	for _, e := range sampled {
		ev := e.Event.NormalizeDynamicEventV04()
		counts[ev.EventInfo.EventType]++
		sampleIDs = append(sampleIDs, ev.Identity.EventID)
	}
	return map[string]any{
		"status":               "ok",
		"from_lsn":             fromLSN,
		"latest_lsn":           r.wal.LatestLSN(),
		"scanned_entries":      total,
		"sampled_entries":      len(sampled),
		"event_type_counts":    counts,
		"sample_event_ids":     sampleIDs,
		"first_sample_lsn":     sampled[0].LSN,
		"last_sample_lsn":      sampled[len(sampled)-1].LSN,
		"replay_apply_enabled": false,
		"note":                 "preview only; no state mutation performed",
	}, nil
}

// AdminReplayApply replays WAL entries by re-submitting events through the ingest path.
// This mutates runtime state and appends new WAL entries for the replayed events.
func (r *Runtime) AdminReplayApply(fromLSN int64, limit int) (map[string]any, error) {
	if r == nil || r.wal == nil {
		return nil, errors.New("wal not configured")
	}
	if fromLSN < 0 {
		fromLSN = 0
	}
	entries, err := eventbackbone.ScanWAL(r.wal, fromLSN)
	if err != nil {
		return nil, fmt.Errorf("scan WAL for replay apply: %w", err)
	}
	total := len(entries)
	if total == 0 {
		return map[string]any{
			"status":           "ok",
			"from_lsn":         fromLSN,
			"latest_lsn":       r.wal.LatestLSN(),
			"scanned_entries":  0,
			"attempted":        0,
			"applied":          0,
			"failed":           0,
			"failed_event_ids": []string{},
			"note":             "no WAL entries to replay",
		}, nil
	}
	if limit <= 0 || limit > total {
		limit = total
	}
	target := entries[:limit]
	applied := 0
	failed := 0
	failedIDs := make([]string, 0)
	for _, entry := range target {
		ev := entry.Event.NormalizeDynamicEventV04()
		if strings.TrimSpace(ev.Identity.EventID) == "" {
			failed++
			failedIDs = append(failedIDs, "")
			continue
		}
		if _, err := r.SubmitIngest(ev); err != nil {
			failed++
			failedIDs = append(failedIDs, ev.Identity.EventID)
			continue
		}
		applied++
	}
	return map[string]any{
		"status":           "ok",
		"from_lsn":         fromLSN,
		"latest_lsn":       r.wal.LatestLSN(),
		"scanned_entries":  total,
		"attempted":        len(target),
		"applied":          applied,
		"failed":           failed,
		"failed_event_ids": failedIDs,
		"note":             "replay apply re-submits events via ingest path",
	}, nil
}
