package access

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

func resolveDocumentAssembledMaxBytes() int64 {
	const defaultMax = 512 * 1024 * 1024 // 512 MiB
	raw := strings.TrimSpace(os.Getenv("PLASMOD_DOCUMENT_UPLOAD_MAX_ASSEMBLED_BYTES"))
	if raw == "" {
		return defaultMax
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n < 1 {
		return defaultMax
	}
	return n
}

func resolveDocumentSegmentPendingMax() int {
	const defaultMax = 1024
	raw := strings.TrimSpace(os.Getenv("PLASMOD_DOCUMENT_SEGMENT_MAX_PENDING"))
	if raw == "" {
		return defaultMax
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return defaultMax
	}
	return n
}

// documentSegmentAssembler buffers UTF-8 text segments uploaded across multiple
// HTTP requests (same upload_batch_id) so each request stays under reverse-proxy
// body limits; full document is assembled before ingest chunking runs.
type documentSegmentAssembler struct {
	mu           sync.Mutex
	maxAssembled int64
	maxPending   int
	ttl          time.Duration
	pending      map[string]*pendingDocSegments
	order        []string
}

type pendingDocSegments struct {
	total      int
	segments   map[int]string
	totalBytes int64
	lastAccess time.Time
}

func newDocumentSegmentAssembler() *documentSegmentAssembler {
	return &documentSegmentAssembler{
		maxAssembled: resolveDocumentAssembledMaxBytes(),
		maxPending:   resolveDocumentSegmentPendingMax(),
		ttl:          45 * time.Minute,
		pending:      make(map[string]*pendingDocSegments),
	}
}

// tryAssembleDocument returns the full text when all segments arrived.
// If segmentTotal <= 1, text is returned unchanged and accumulating is nil.
// When accumulating is non-nil the handler should JSON-encode it and return.
func (a *documentSegmentAssembler) tryAssembleDocument(uploadBatchID string, segmentIndex, segmentTotal int, text string) (full string, accumulating map[string]any, err error) {
	if segmentTotal <= 1 {
		return text, nil, nil
	}
	if strings.TrimSpace(uploadBatchID) == "" {
		return "", nil, fmt.Errorf("upload_batch_id is required when segment_total > 1")
	}
	if segmentIndex < 0 || segmentIndex >= segmentTotal {
		return "", nil, fmt.Errorf("segment_index must be in [0, segment_total)")
	}
	if int64(len(text)) > a.maxAssembled {
		return "", nil, fmt.Errorf("segment exceeds PLASMOD_DOCUMENT_UPLOAD_MAX_ASSEMBLED_BYTES (%d)", a.maxAssembled)
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	a.evictExpiredLocked()

	p, ok := a.pending[uploadBatchID]
	if !ok {
		for len(a.pending) >= a.maxPending {
			a.evictOldestLocked()
		}
		p = &pendingDocSegments{
			total:      segmentTotal,
			segments:   make(map[int]string),
			lastAccess: time.Now(),
		}
		a.pending[uploadBatchID] = p
		a.order = append(a.order, uploadBatchID)
	}
	if p.total != segmentTotal {
		return "", nil, fmt.Errorf("segment_total mismatch for this upload_batch_id (expected %d, got %d)", p.total, segmentTotal)
	}

	if prev, exists := p.segments[segmentIndex]; exists {
		p.totalBytes -= int64(len(prev))
	}
	p.segments[segmentIndex] = text
	p.totalBytes += int64(len(text))
	p.lastAccess = time.Now()

	if p.totalBytes > a.maxAssembled {
		delete(a.pending, uploadBatchID)
		a.removeFromOrderLocked(uploadBatchID)
		return "", nil, fmt.Errorf("assembled document would exceed max size (%d bytes)", a.maxAssembled)
	}

	if !documentSegmentsComplete(p) {
		return "", map[string]any{
			"status":             "accumulating",
			"upload_batch_id":    uploadBatchID,
			"segments_received":  len(p.segments),
			"segments_total":     p.total,
			"last_segment_index": segmentIndex,
		}, nil
	}

	var b strings.Builder
	b.Grow(int(p.totalBytes))
	for i := 0; i < p.total; i++ {
		s := p.segments[i]
		b.WriteString(s)
	}
	delete(a.pending, uploadBatchID)
	a.removeFromOrderLocked(uploadBatchID)
	return b.String(), nil, nil
}

func (a *documentSegmentAssembler) evictExpiredLocked() {
	now := time.Now()
	for k, p := range a.pending {
		if now.Sub(p.lastAccess) > a.ttl {
			delete(a.pending, k)
			a.removeFromOrderLocked(k)
		}
	}
}

func (a *documentSegmentAssembler) evictOldestLocked() {
	if len(a.order) == 0 {
		return
	}
	oldest := a.order[0]
	delete(a.pending, oldest)
	a.order = a.order[1:]
}

func (a *documentSegmentAssembler) removeFromOrderLocked(id string) {
	for i, k := range a.order {
		if k == id {
			a.order = append(a.order[:i], a.order[i+1:]...)
			return
		}
	}
}

func documentSegmentsComplete(p *pendingDocSegments) bool {
	for i := 0; i < p.total; i++ {
		if _, ok := p.segments[i]; !ok {
			return false
		}
	}
	return true
}
