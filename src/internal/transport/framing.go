package transport

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
)

// Wire-format constants. All multi-byte fields are little-endian.
//
//	IngestBatch request (ver 1 — legacy):
//	  [magic(4)='PLIB'][ver(1)=1]
//	  [seg_id_len(u16)][seg_id_bytes]
//	  [n(u32)][dim(u32)]
//	  [n*dim*float32]
//	  [for i in 0..n-1: id_len(u16) id_bytes]
//
//	IngestBatch request (ver 2 — benchmark):
//	  [magic(4)='PLIB'][ver(1)=2]
//	  [seg_id_len(u16)][seg_id_bytes]
//	  [indexed_count(u32)][dim(u32)]
//	  [indexed_count*dim*float32]
//	  [for i in 0..indexed_count-1: id_len(u16) id_bytes]
//
//	QueryWarm request:
//	  [magic(4)='PLQW'][ver(1)=1]
//	  [seg_id_len(u16)][seg_id_bytes]
//	  [topk(u32)][dim(u32)]
//	  [dim*float32]
//
//	QueryWarm response (binary):
//	  [n(u32)]
//	  [for i in 0..n-1: id_len(u16) id_bytes]
const (
	magicIngestBatch    = "PLIB"
	magicQueryWarm      = "PLQW"
	magicQueryWarmBatch = "PLQB"
	wireVersion         = byte(1)
	wireVersion2        = byte(2)

	maxBatchVectors = 1 << 22 // 4M vectors / request
	maxDim          = 1 << 14 // 16384
	maxIDLen        = 1 << 12 // 4096 bytes per object id
	maxQueryBatch   = 1 << 16 // 65536 queries / batch request
)

// IngestBatch is the decoded payload of an ingest_batch binary request.
type IngestBatch struct {
	SegmentID string
	Dim       int
	ObjectIDs []string
	Vectors   [][]float32
}

// QueryWarm is the decoded payload of a query_warm binary request.
type QueryWarm struct {
	SegmentID string
	TopK      int
	Vector    []float32
}

// QueryWarmBatch is the decoded payload of a batch query binary request.
// nq queries are packed consecutively: [q0_dim*4][q1_dim*4]...[qnq-1_dim*4]
type QueryWarmBatch struct {
	SegmentID string
	TopK      int
	NQ        int
	Dim       int
	// Flat row-major float32 array: queries[0], queries[1], ..., queries[nq-1]
	Queries []float32
}

func readExact(r io.Reader, buf []byte) error {
	_, err := io.ReadFull(r, buf)
	return err
}

func readU16(r io.Reader) (uint16, error) {
	var b [2]byte
	if err := readExact(r, b[:]); err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint16(b[:]), nil
}

func readU32(r io.Reader) (uint32, error) {
	var b [4]byte
	if err := readExact(r, b[:]); err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(b[:]), nil
}

func readString(r io.Reader, n int) (string, error) {
	if n == 0 {
		return "", nil
	}
	buf := make([]byte, n)
	if err := readExact(r, buf); err != nil {
		return "", err
	}
	return string(buf), nil
}

// DecodeIngestBatch reads a binary IngestBatch request from r.
// Supports wire version 1 (all vectors = indexed) and version 2 (indexed_count field).
func DecodeIngestBatch(r io.Reader) (*IngestBatch, error) {
	var hdr [5]byte
	if err := readExact(r, hdr[:]); err != nil {
		return nil, fmt.Errorf("read header: %w", err)
	}
	if string(hdr[:4]) != magicIngestBatch {
		return nil, errors.New("invalid magic for ingest_batch")
	}
	ver := hdr[4]
	if ver != wireVersion && ver != wireVersion2 {
		return nil, fmt.Errorf("unsupported wire version %d", ver)
	}

	segLen, err := readU16(r)
	if err != nil {
		return nil, err
	}
	if int(segLen) > maxIDLen {
		return nil, fmt.Errorf("segment_id too long: %d", segLen)
	}
	segID, err := readString(r, int(segLen))
	if err != nil {
		return nil, err
	}

	n32, err := readU32(r)
	if err != nil {
		return nil, err
	}
	dim32, err := readU32(r)
	if err != nil {
		return nil, err
	}
	n, dim := int(n32), int(dim32)
	if n <= 0 || n > maxBatchVectors {
		return nil, fmt.Errorf("invalid n=%d", n)
	}
	if dim <= 0 || dim > maxDim {
		return nil, fmt.Errorf("invalid dim=%d", dim)
	}

	// Read raw float32 blob in one shot for cache friendliness.
	rawLen := n * dim * 4
	raw := make([]byte, rawLen)
	if err := readExact(r, raw); err != nil {
		return nil, fmt.Errorf("read vectors: %w", err)
	}
	vectors := make([][]float32, n)
	off := 0
	for i := 0; i < n; i++ {
		v := make([]float32, dim)
		for j := 0; j < dim; j++ {
			v[j] = math.Float32frombits(binary.LittleEndian.Uint32(raw[off : off+4]))
			off += 4
		}
		vectors[i] = v
	}

	ids := make([]string, n)
	for i := 0; i < n; i++ {
		l, err := readU16(r)
		if err != nil {
			return nil, fmt.Errorf("read id[%d] len: %w", i, err)
		}
		if int(l) > maxIDLen {
			return nil, fmt.Errorf("id[%d] too long: %d", i, l)
		}
		s, err := readString(r, int(l))
		if err != nil {
			return nil, err
		}
		ids[i] = s
	}

	return &IngestBatch{
		SegmentID: segID,
		Dim:       dim,
		ObjectIDs: ids,
		Vectors:   vectors,
	}, nil
}

// DecodeQueryWarm reads a binary QueryWarm request from r.
func DecodeQueryWarm(r io.Reader) (*QueryWarm, error) {
	var hdr [5]byte
	if err := readExact(r, hdr[:]); err != nil {
		return nil, fmt.Errorf("read header: %w", err)
	}
	if string(hdr[:4]) != magicQueryWarm {
		return nil, errors.New("invalid magic for query_warm")
	}
	if hdr[4] != wireVersion {
		return nil, fmt.Errorf("unsupported wire version %d", hdr[4])
	}

	segLen, err := readU16(r)
	if err != nil {
		return nil, err
	}
	segID, err := readString(r, int(segLen))
	if err != nil {
		return nil, err
	}

	topK32, err := readU32(r)
	if err != nil {
		return nil, err
	}
	dim32, err := readU32(r)
	if err != nil {
		return nil, err
	}
	dim := int(dim32)
	if dim <= 0 || dim > maxDim {
		return nil, fmt.Errorf("invalid dim=%d", dim)
	}

	raw := make([]byte, dim*4)
	if err := readExact(r, raw); err != nil {
		return nil, err
	}
	vec := make([]float32, dim)
	for j := 0; j < dim; j++ {
		vec[j] = math.Float32frombits(binary.LittleEndian.Uint32(raw[j*4 : j*4+4]))
	}

	return &QueryWarm{
		SegmentID: segID,
		TopK:      int(topK32),
		Vector:    vec,
	}, nil
}

// EncodeQueryWarmResponse writes [n(u32)][n*(idlen u16, id bytes)] to w.
func EncodeQueryWarmResponse(w io.Writer, ids []string) error {
	var hdr [4]byte
	binary.LittleEndian.PutUint32(hdr[:], uint32(len(ids)))
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	var lenBuf [2]byte
	for _, id := range ids {
		if len(id) > maxIDLen {
			return fmt.Errorf("id too long: %d", len(id))
		}
		binary.LittleEndian.PutUint16(lenBuf[:], uint16(len(id)))
		if _, err := w.Write(lenBuf[:]); err != nil {
			return err
		}
		if len(id) > 0 {
			if _, err := io.WriteString(w, id); err != nil {
				return err
			}
		}
	}
	return nil
}

// DecodeQueryWarmBatch reads a binary batch-query request.
//
// Wire format:
//   [magic='PLQB'(4)][ver(1)=1]
//   [seg_id_len(u16)][seg_id_bytes]
//   [topk(u32)][nq(u32)][dim(u32)]
//   [nq*dim*float32]  — row-major flat array
func DecodeQueryWarmBatch(r io.Reader) (*QueryWarmBatch, error) {
	var hdr [5]byte
	if err := readExact(r, hdr[:]); err != nil {
		return nil, fmt.Errorf("read header: %w", err)
	}
	if string(hdr[:4]) != magicQueryWarmBatch {
		return nil, errors.New("invalid magic for batch query_warm")
	}
	if hdr[4] != wireVersion {
		return nil, fmt.Errorf("unsupported wire version %d", hdr[4])
	}

	segLen, err := readU16(r)
	if err != nil {
		return nil, err
	}
	segID, err := readString(r, int(segLen))
	if err != nil {
		return nil, err
	}

	topK32, err := readU32(r)
	if err != nil {
		return nil, err
	}
	nq32, err := readU32(r)
	if err != nil {
		return nil, err
	}
	dim32, err := readU32(r)
	if err != nil {
		return nil, err
	}
	nq := int(nq32)
	dim := int(dim32)

	if nq <= 0 || nq > maxQueryBatch {
		return nil, fmt.Errorf("invalid nq=%d", nq)
	}
	if dim <= 0 || dim > maxDim {
		return nil, fmt.Errorf("invalid dim=%d", dim)
	}

	totalBytes := nq * dim * 4
	raw := make([]byte, totalBytes)
	if err := readExact(r, raw); err != nil {
		return nil, fmt.Errorf("read queries: %w", err)
	}

	vecs := make([]float32, nq*dim)
	off := 0
	for i := 0; i < nq*dim; i++ {
		vecs[i] = math.Float32frombits(binary.LittleEndian.Uint32(raw[off : off+4]))
		off += 4
	}

	return &QueryWarmBatch{
		SegmentID: segID,
		TopK:      int(topK32),
		NQ:        nq,
		Dim:       dim,
		Queries:   vecs,
	}, nil
}

// QueryWarmBatchResponse holds decoded batch search results (integer indices only).
type QueryWarmBatchResponse struct {
	NQ    int
	TopK  int
	IDs   []int64 // length nq*topk, row-major: [q0_ids][q1_ids]...
	Dists []float32
}

// EncodeQueryWarmBatchResponse writes a batch response in binary format:
//
//   [nq(u32)][topk(u32)]
//   [nq*topk * int64]   — flat row-major integer indices
//   [nq*topk * float32]  — flat row-major distances
func EncodeQueryWarmBatchResponse(w io.Writer, resp *QueryWarmBatchResponse) error {
	var hdr [8]byte
	binary.LittleEndian.PutUint32(hdr[:4], uint32(resp.NQ))
	binary.LittleEndian.PutUint32(hdr[4:8], uint32(resp.TopK))
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	// Write IDs as raw bytes
	if len(resp.IDs) > 0 {
		idBytes := make([]byte, len(resp.IDs)*8)
		for i, id := range resp.IDs {
			binary.LittleEndian.PutUint64(idBytes[i*8:i*8+8], uint64(id))
		}
		if _, err := w.Write(idBytes); err != nil {
			return err
		}
	}
	// Write dists as raw bytes
	if len(resp.Dists) > 0 {
		distBytes := make([]byte, len(resp.Dists)*4)
		for i, d := range resp.Dists {
			binary.LittleEndian.PutUint32(distBytes[i*4:i*4+4], math.Float32bits(d))
		}
		if _, err := w.Write(distBytes); err != nil {
			return err
		}
	}
	return nil
}
