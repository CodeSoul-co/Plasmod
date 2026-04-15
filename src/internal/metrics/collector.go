// Package metrics provides a lightweight, dependency-free runtime metrics
// collector for Plasmod.  It is designed to be wired into the gateway and
// runtime without any external Prometheus or OpenTelemetry dependency.
//
// Covered experiment IDs (server-side observability surface):
//
//	3-MS1  backend query latency      → QueryLatency histogram
//	3-MS2  write latency              → WriteLatency histogram
//	3-MS3  write-to-visible latency   → WriteToVisibleLatency histogram
//	3-MS4  storage growth             → StorageGrowth counter (updated by gateway)
//	3-MS5  retrieval error rate       → RetrievalErrors / QueryTotal
//	3-MS6  resource overhead          → Go runtime MemStats snapshot
//	1-M8   memory footprint           → same MemStats snapshot
//	1-M10  scale-out efficiency       → IngestTotal / QueryTotal throughput counters
//	1-T7   node scaling               → ConcurrentQueries gauge
//	3-MT1  task success rate          → TaskSuccess / TaskTotal per session
//	3-MT2  avg steps to completion    → TaskSteps per session
//	3-MT3  completion time            → TaskDuration histogram
//	3-MT4  token cost                 → TaskTokens per session
//	3-MT5  evidence-supported rate    → EvidenceSupported / QueryTotal
//	3-MT6  plan repair success rate   → PlanRepairOK / PlanRepairTotal
//	3-MT7  hallucination rate         → UnsupportedClaims / QueryTotal
//	4-M1   cross-agent contamination  → ContaminationAttempts counter
//	4-M4   conflict preservation rate → ConflictsPreserved / ConflictsTotal
//	4-M5   final answer consistency   → AnswerConsistencyScores accumulator
//	4-M6   MAS task success rate      → MASTaskSuccess / MASTaskTotal
package metrics

import (
	"math"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// ── Latency histogram ────────────────────────────────────────────────────────

// histogram is a concurrent, fixed-bucket latency histogram (milliseconds).
type histogram struct {
	mu      sync.Mutex
	samples []float64
}

func (h *histogram) record(ms float64) {
	h.mu.Lock()
	h.samples = append(h.samples, ms)
	h.mu.Unlock()
}

// percentiles returns p50, p95, p99 over all recorded samples.
func (h *histogram) percentiles() (p50, p95, p99 float64) {
	h.mu.Lock()
	cp := make([]float64, len(h.samples))
	copy(cp, h.samples)
	h.mu.Unlock()

	if len(cp) == 0 {
		return 0, 0, 0
	}
	sort.Float64s(cp)
	idx := func(pct float64) float64 {
		i := int(math.Ceil(pct/100.0*float64(len(cp)))) - 1
		if i < 0 {
			i = 0
		}
		if i >= len(cp) {
			i = len(cp) - 1
		}
		return cp[i]
	}
	return idx(50), idx(95), idx(99)
}

func (h *histogram) count() int {
	h.mu.Lock()
	n := len(h.samples)
	h.mu.Unlock()
	return n
}

// ── Task session state ───────────────────────────────────────────────────────

// TaskRecord accumulates per-session task metrics (3-MT1 ~ 3-MT7).
type TaskRecord struct {
	mu              sync.Mutex
	steps           int64
	tokens          int64
	evidenceQueries int64 // queries that had at least one evidence object
	totalQueries    int64
	planRepairOK    int64
	planRepairTotal int64
	unsupported     int64 // hallucinated / unsupported claims
	success         bool
	durationMs      float64
	done            bool
}

func (t *TaskRecord) AddStep() {
	t.mu.Lock()
	t.steps++
	t.mu.Unlock()
}

func (t *TaskRecord) AddTokens(n int64) {
	t.mu.Lock()
	t.tokens += n
	t.mu.Unlock()
}

func (t *TaskRecord) RecordQuery(evidenceSupported bool) {
	t.mu.Lock()
	t.totalQueries++
	if evidenceSupported {
		t.evidenceQueries++
	}
	t.mu.Unlock()
}

func (t *TaskRecord) RecordPlanRepair(ok bool) {
	t.mu.Lock()
	t.planRepairTotal++
	if ok {
		t.planRepairOK++
	}
	t.mu.Unlock()
}

func (t *TaskRecord) RecordUnsupportedClaim() {
	t.mu.Lock()
	t.unsupported++
	t.mu.Unlock()
}

func (t *TaskRecord) Complete(success bool, durationMs float64) {
	t.mu.Lock()
	t.success = success
	t.durationMs = durationMs
	t.done = true
	t.mu.Unlock()
}

// Snapshot returns a point-in-time copy of the task record.
func (t *TaskRecord) Snapshot() TaskSnapshot {
	t.mu.Lock()
	defer t.mu.Unlock()
	evidenceRate := float64(0)
	if t.totalQueries > 0 {
		evidenceRate = float64(t.evidenceQueries) / float64(t.totalQueries)
	}
	repairRate := float64(0)
	if t.planRepairTotal > 0 {
		repairRate = float64(t.planRepairOK) / float64(t.planRepairTotal)
	}
	hallucinationRate := float64(0)
	if t.totalQueries > 0 {
		hallucinationRate = float64(t.unsupported) / float64(t.totalQueries)
	}
	return TaskSnapshot{
		Steps:                   t.steps,
		Tokens:                  t.tokens,
		TotalQueries:            t.totalQueries,
		EvidenceSupportedRate:   evidenceRate,
		PlanRepairSuccessRate:   repairRate,
		HallucinationRate:       hallucinationRate,
		Success:                 t.success,
		DurationMs:              t.durationMs,
		Done:                    t.done,
	}
}

// TaskSnapshot is the serialisable form of a TaskRecord.
type TaskSnapshot struct {
	Steps                 int64   `json:"steps"`
	Tokens                int64   `json:"tokens"`
	TotalQueries          int64   `json:"total_queries"`
	EvidenceSupportedRate float64 `json:"evidence_supported_rate"`
	PlanRepairSuccessRate float64 `json:"plan_repair_success_rate"`
	HallucinationRate     float64 `json:"hallucination_rate"`
	Success               bool    `json:"success"`
	DurationMs            float64 `json:"duration_ms"`
	Done                  bool    `json:"done"`
}

// ── Rolling throughput window ────────────────────────────────────────────────

// throughputWindow tracks per-second event counts over the last windowSecs
// seconds using a circular bucket array.  Thread-safe.
const throughputWindowSecs = 60

type throughputWindow struct {
	mu      sync.Mutex
	buckets [throughputWindowSecs]int64
	ts      [throughputWindowSecs]int64 // unix second for each bucket
}

func (tw *throughputWindow) Inc() {
	now := time.Now().Unix()
	slot := now % throughputWindowSecs
	tw.mu.Lock()
	if tw.ts[slot] != now {
		tw.buckets[slot] = 0
		tw.ts[slot] = now
	}
	tw.buckets[slot]++
	tw.mu.Unlock()
}

// Rate returns the total count in the last n seconds (n ≤ windowSecs).
func (tw *throughputWindow) Rate(n int) int64 {
	if n > throughputWindowSecs {
		n = throughputWindowSecs
	}
	now := time.Now().Unix()
	tw.mu.Lock()
	defer tw.mu.Unlock()
	var total int64
	for i := 0; i < n; i++ {
		sec := now - int64(i)
		slot := sec % throughputWindowSecs
		if tw.ts[slot] == sec {
			total += tw.buckets[slot]
		}
	}
	return total
}

// ── Main Collector ───────────────────────────────────────────────────────────

// Collector is the central thread-safe metrics store.
type Collector struct {
	// Latency histograms (milliseconds)
	QueryLatency         histogram
	WriteLatency         histogram
	WriteToVisibleLatency histogram
	TaskDuration         histogram

	// Rolling throughput windows (1-T7, 1-M10)
	IngestRate throughputWindow
	QueryRate  throughputWindow

	// Throughput counters
	IngestTotal      atomic.Int64
	QueryTotal       atomic.Int64
	RetrievalErrors  atomic.Int64

	// Quality counters
	EvidenceSupported  atomic.Int64 // queries with at least one evidence obj
	PlanRepairOK       atomic.Int64
	PlanRepairTotal    atomic.Int64
	UnsupportedClaims  atomic.Int64

	// Concurrency gauge
	ConcurrentQueries atomic.Int64

	// Storage growth (bytes reported externally)
	StorageBytesTotal    atomic.Int64
	StorageMemoryCount   atomic.Int64
	StorageEventCount    atomic.Int64

	// MAS metrics (4-M1, 4-M4, 4-M5, 4-M6)
	ContaminationAttempts  atomic.Int64
	ConflictsPreserved     atomic.Int64
	ConflictsTotal         atomic.Int64
	MASTaskSuccess         atomic.Int64
	MASTaskTotal           atomic.Int64

	// Answer consistency accumulator (sum + count → mean)
	answerConsistencyMu    sync.Mutex
	answerConsistencySum   float64
	answerConsistencyCount int64

	// Per-session task records
	taskMu  sync.RWMutex
	tasks   map[string]*TaskRecord
}

// NewCollector allocates a ready-to-use Collector.
func NewCollector() *Collector {
	return &Collector{tasks: make(map[string]*TaskRecord)}
}

// ── Global singleton ─────────────────────────────────────────────────────────

var (
	globalOnce      sync.Once
	globalCollector *Collector
)

// Global returns the process-wide singleton Collector.
func Global() *Collector {
	globalOnce.Do(func() { globalCollector = NewCollector() })
	return globalCollector
}

// ── Recording helpers ────────────────────────────────────────────────────────

func (c *Collector) RecordQueryLatency(d time.Duration) {
	c.QueryLatency.record(float64(d.Milliseconds()))
	c.QueryTotal.Add(1)
	c.QueryRate.Inc()
}

func (c *Collector) RecordWriteLatency(d time.Duration) {
	c.WriteLatency.record(float64(d.Milliseconds()))
	c.IngestTotal.Add(1)
	c.IngestRate.Inc()
}

func (c *Collector) RecordWriteToVisible(d time.Duration) {
	c.WriteToVisibleLatency.record(float64(d.Milliseconds()))
}

func (c *Collector) RecordRetrievalError() {
	c.RetrievalErrors.Add(1)
}

func (c *Collector) RecordEvidenceSupported(supported bool) {
	if supported {
		c.EvidenceSupported.Add(1)
	}
}

func (c *Collector) RecordPlanRepair(ok bool) {
	c.PlanRepairTotal.Add(1)
	if ok {
		c.PlanRepairOK.Add(1)
	}
}

func (c *Collector) RecordUnsupportedClaim() {
	c.UnsupportedClaims.Add(1)
}

func (c *Collector) RecordTaskDuration(ms float64) {
	c.TaskDuration.record(ms)
}

func (c *Collector) RecordContaminationAttempt() {
	c.ContaminationAttempts.Add(1)
}

func (c *Collector) RecordConflict(preserved bool) {
	c.ConflictsTotal.Add(1)
	if preserved {
		c.ConflictsPreserved.Add(1)
	}
}

func (c *Collector) RecordAnswerConsistency(score float64) {
	c.answerConsistencyMu.Lock()
	c.answerConsistencySum += score
	c.answerConsistencyCount++
	c.answerConsistencyMu.Unlock()
}

func (c *Collector) RecordMASTask(success bool) {
	c.MASTaskTotal.Add(1)
	if success {
		c.MASTaskSuccess.Add(1)
	}
}

// ── Per-session task helpers ─────────────────────────────────────────────────

// Session returns (or creates) the TaskRecord for the given sessionID.
func (c *Collector) Session(sessionID string) *TaskRecord {
	c.taskMu.RLock()
	if tr, ok := c.tasks[sessionID]; ok {
		c.taskMu.RUnlock()
		return tr
	}
	c.taskMu.RUnlock()

	c.taskMu.Lock()
	defer c.taskMu.Unlock()
	if tr, ok := c.tasks[sessionID]; ok {
		return tr
	}
	tr := &TaskRecord{}
	c.tasks[sessionID] = tr
	return tr
}

// SessionSnapshot returns the current snapshot for a session, and whether
// the session exists.
func (c *Collector) SessionSnapshot(sessionID string) (TaskSnapshot, bool) {
	c.taskMu.RLock()
	tr, ok := c.tasks[sessionID]
	c.taskMu.RUnlock()
	if !ok {
		return TaskSnapshot{}, false
	}
	return tr.Snapshot(), true
}

// AllSessionSnapshots returns snapshots for every tracked session.
func (c *Collector) AllSessionSnapshots() map[string]TaskSnapshot {
	c.taskMu.RLock()
	keys := make([]string, 0, len(c.tasks))
	for k := range c.tasks {
		keys = append(keys, k)
	}
	c.taskMu.RUnlock()

	out := make(map[string]TaskSnapshot, len(keys))
	for _, k := range keys {
		if snap, ok := c.SessionSnapshot(k); ok {
			out[k] = snap
		}
	}
	return out
}

// ── Snapshot ─────────────────────────────────────────────────────────────────

// LatencyStats holds p50/p95/p99 and sample count for one histogram.
type LatencyStats struct {
	P50Ms  float64 `json:"p50_ms"`
	P95Ms  float64 `json:"p95_ms"`
	P99Ms  float64 `json:"p99_ms"`
	Count  int     `json:"count"`
}

// Snapshot is a serialisable point-in-time view of all metrics.
type Snapshot struct {
	// 3-MS1
	QueryLatency LatencyStats `json:"query_latency"`
	// 3-MS2
	WriteLatency LatencyStats `json:"write_latency"`
	// 3-MS3
	WriteToVisibleLatency LatencyStats `json:"write_to_visible_latency"`
	// 3-MT3
	TaskDuration LatencyStats `json:"task_duration"`

	// 1-M10 / 1-T7
	IngestTotal          int64 `json:"ingest_total"`
	QueryTotal           int64 `json:"query_total"`
	ConcurrentQueries    int64 `json:"concurrent_queries"`
	IngestPerSec1m       int64 `json:"ingest_per_sec_1m"`
	QueryPerSec1m        int64 `json:"query_per_sec_1m"`
	IngestPerSec10s      int64 `json:"ingest_per_sec_10s"`
	QueryPerSec10s       int64 `json:"query_per_sec_10s"`

	// 3-MS5
	RetrievalErrors     int64   `json:"retrieval_errors"`
	RetrievalErrorRate  float64 `json:"retrieval_error_rate"`

	// 3-MT5
	EvidenceSupported     int64   `json:"evidence_supported"`
	EvidenceSupportedRate float64 `json:"evidence_supported_rate"`

	// 3-MT6
	PlanRepairOK    int64   `json:"plan_repair_ok"`
	PlanRepairTotal int64   `json:"plan_repair_total"`
	PlanRepairRate  float64 `json:"plan_repair_rate"`

	// 3-MT7
	UnsupportedClaims     int64   `json:"unsupported_claims"`
	HallucinationRate     float64 `json:"hallucination_rate"`

	// 3-MS4 / 1-M8
	StorageBytesTotal  int64 `json:"storage_bytes_total"`
	StorageMemoryCount int64 `json:"storage_memory_count"`
	StorageEventCount  int64 `json:"storage_event_count"`

	// 4-M1
	ContaminationAttempts int64 `json:"contamination_attempts"`

	// 4-M4
	ConflictsPreserved int64   `json:"conflicts_preserved"`
	ConflictsTotal     int64   `json:"conflicts_total"`
	ConflictPreservationRate float64 `json:"conflict_preservation_rate"`

	// 4-M5
	AnswerConsistencyMean float64 `json:"answer_consistency_mean"`

	// 4-M6
	MASTaskSuccess int64   `json:"mas_task_success"`
	MASTaskTotal   int64   `json:"mas_task_total"`
	MASTaskRate    float64 `json:"mas_task_success_rate"`

	// 3-MS6 / 1-M8  Go runtime memory
	GoAllocBytes    uint64 `json:"go_alloc_bytes"`
	GoSysBytes      uint64 `json:"go_sys_bytes"`
	GoGoroutines    int    `json:"go_goroutines"`
	GoNumGC         uint32 `json:"go_num_gc"`

	// Per-session task metrics (3-MT1~MT7)
	Sessions map[string]TaskSnapshot `json:"sessions,omitempty"`

	CollectedAt string `json:"collected_at"`
}

// ratio safely computes a/b, returning 0 when b==0.
func ratio(a, b int64) float64 {
	if b == 0 {
		return 0
	}
	return float64(a) / float64(b)
}

func latStats(h *histogram) LatencyStats {
	p50, p95, p99 := h.percentiles()
	return LatencyStats{P50Ms: p50, P95Ms: p95, P99Ms: p99, Count: h.count()}
}

// Snapshot computes and returns a point-in-time metrics snapshot.
func (c *Collector) Snapshot() Snapshot {
	queryTotal := c.QueryTotal.Load()
	ingestTotal := c.IngestTotal.Load()
	retriErr := c.RetrievalErrors.Load()
	evidSup := c.EvidenceSupported.Load()
	prOK := c.PlanRepairOK.Load()
	prTotal := c.PlanRepairTotal.Load()
	unsup := c.UnsupportedClaims.Load()
	confPres := c.ConflictsPreserved.Load()
	confTotal := c.ConflictsTotal.Load()
	masOK := c.MASTaskSuccess.Load()
	masTotal := c.MASTaskTotal.Load()

	c.answerConsistencyMu.Lock()
	acSum := c.answerConsistencySum
	acCount := c.answerConsistencyCount
	c.answerConsistencyMu.Unlock()
	acMean := float64(0)
	if acCount > 0 {
		acMean = acSum / float64(acCount)
	}

	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)

	return Snapshot{
		QueryLatency:          latStats(&c.QueryLatency),
		WriteLatency:          latStats(&c.WriteLatency),
		WriteToVisibleLatency: latStats(&c.WriteToVisibleLatency),
		TaskDuration:          latStats(&c.TaskDuration),

		IngestTotal:          ingestTotal,
		QueryTotal:           queryTotal,
		ConcurrentQueries:    c.ConcurrentQueries.Load(),
		IngestPerSec1m:       c.IngestRate.Rate(60),
		QueryPerSec1m:        c.QueryRate.Rate(60),
		IngestPerSec10s:      c.IngestRate.Rate(10),
		QueryPerSec10s:       c.QueryRate.Rate(10),

		RetrievalErrors:    retriErr,
		RetrievalErrorRate: ratio(retriErr, queryTotal),

		EvidenceSupported:     evidSup,
		EvidenceSupportedRate: ratio(evidSup, queryTotal),

		PlanRepairOK:    prOK,
		PlanRepairTotal: prTotal,
		PlanRepairRate:  ratio(prOK, prTotal),

		UnsupportedClaims: unsup,
		HallucinationRate: ratio(unsup, queryTotal),

		StorageBytesTotal:  c.StorageBytesTotal.Load(),
		StorageMemoryCount: c.StorageMemoryCount.Load(),
		StorageEventCount:  c.StorageEventCount.Load(),

		ContaminationAttempts: c.ContaminationAttempts.Load(),

		ConflictsPreserved:       confPres,
		ConflictsTotal:           confTotal,
		ConflictPreservationRate: ratio(confPres, confTotal),

		AnswerConsistencyMean: acMean,

		MASTaskSuccess: masOK,
		MASTaskTotal:   masTotal,
		MASTaskRate:    ratio(masOK, masTotal),

		GoAllocBytes: ms.Alloc,
		GoSysBytes:   ms.Sys,
		GoGoroutines: runtime.NumGoroutine(),
		GoNumGC:      ms.NumGC,

		Sessions:    c.AllSessionSnapshots(),
		CollectedAt: time.Now().UTC().Format(time.RFC3339),
	}
}
