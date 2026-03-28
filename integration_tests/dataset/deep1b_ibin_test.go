// Package dataset hosts file-level tests for local fixtures (no ANDB server required).
package dataset_test

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestDeep1BIbin validates fixtures/member_a/dataset/deep1B.ibin:
// uint32 LE n, uint32 LE dim, then n*dim float32 row-major.
//
// Does not call the HTTP API. Default ANDB embedders (e.g. TF-IDF dim 256) do not match dim=100;
// full retrieval integration needs a precomputed-vector path in the server.
func TestDeep1BIbin(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(file)
	path := filepath.Join(dir, "..", "fixtures", "member_a", "dataset", "deep1B.ibin")
	path = filepath.Clean(path)

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if len(raw) < 8 {
		t.Fatalf("file too small")
	}
	n := binary.LittleEndian.Uint32(raw[0:4])
	dim := binary.LittleEndian.Uint32(raw[4:8])
	if n == 0 || dim == 0 {
		t.Fatalf("invalid header n=%d dim=%d", n, dim)
	}
	wantPayload := int(n) * int(dim) * 4
	if len(raw)-8 != wantPayload {
		t.Fatalf("payload size: got %d want %d (n=%d dim=%d)", len(raw)-8, wantPayload, n, dim)
	}

	vec := make([]float32, dim)
	for i := range vec {
		off := 8 + i*4
		vec[i] = math.Float32frombits(binary.LittleEndian.Uint32(raw[off : off+4]))
	}
	var sq float64
	for _, v := range vec {
		if !isFinite32(v) {
			t.Fatalf("non-finite in first vector: %v", v)
		}
		sq += float64(v) * float64(v)
	}
	t.Logf("deep1B.ibin: n=%d dim=%d L2(first)=%.6f", n, dim, math.Sqrt(sq))

	sampleCount := 1000
	maxFloats := int(n) * int(dim)
	if maxFloats < sampleCount {
		sampleCount = maxFloats
	}
	base := 8 + (len(raw)-8)/2
	for j := 0; j < sampleCount && base+j*4+4 <= len(raw); j++ {
		v := math.Float32frombits(binary.LittleEndian.Uint32(raw[base+j*4 : base+j*4+4]))
		if !isFinite32(v) {
			t.Fatalf("non-finite at mid-file offset %d", j)
		}
	}
}

func isFinite32(f float32) bool {
	return !math.IsNaN(float64(f)) && !math.IsInf(float64(f), 0)
}
