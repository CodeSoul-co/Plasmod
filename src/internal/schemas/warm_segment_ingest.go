package schemas

import (
	"fmt"
	"strings"
)

// Supported warm-segment ANN index types (aligned with plasmod_segment_build_with_type).
const (
	WarmIndexHNSW     = "HNSW"
	WarmIndexIVFFlat  = "IVF_FLAT"
	WarmIndexIVFPQ    = "IVF_PQ"
	WarmIndexIVFSQ8   = "IVF_SQ8"
	WarmIndexDiskANN  = "DISKANN"
)

// WarmVectorsIngestRequest is the JSON body for POST /v1/ingest/vectors.
type WarmVectorsIngestRequest struct {
	SegmentID string      `json:"segment_id,omitempty"`
	ObjectIDs []string    `json:"object_ids,omitempty"`
	Vectors   [][]float32 `json:"vectors"`
	// IndexType selects the ANN index built for this segment. Empty → HNSW.
	IndexType string `json:"index_type,omitempty"`
	// IVF build tuning (0 = C++/Knowhere defaults).
	IVFNlist  int    `json:"ivf_nlist,omitempty"`
	IVFNprobe int    `json:"ivf_nprobe,omitempty"`
	IVFM      int    `json:"ivf_m,omitempty"`     // IVF_PQ sub-vectors
	IVFNbits  int    `json:"ivf_nbits,omitempty"` // IVF_PQ bits per sub-vector
	IVFSqType string `json:"ivf_sq_type,omitempty"` // IVF_SQ8: INT8 | FP32
}

// NormalizeWarmIndexType uppercases and validates index_type. Empty string → HNSW.
func NormalizeWarmIndexType(indexType string) (string, error) {
	t := strings.TrimSpace(strings.ToUpper(indexType))
	if t == "" {
		return WarmIndexHNSW, nil
	}
	switch t {
	case WarmIndexHNSW, WarmIndexIVFFlat, WarmIndexIVFPQ, WarmIndexIVFSQ8, WarmIndexDiskANN:
		return t, nil
	default:
		return "", fmt.Errorf(
			"index_type must be one of HNSW, IVF_FLAT, IVF_PQ, IVF_SQ8, DISKANN (got %q)",
			indexType,
		)
	}
}
