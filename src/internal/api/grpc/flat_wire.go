package grpcapi

import (
	"fmt"
	"unsafe"
)

const (
	grpcMaxBatchVectors = 1 << 22
	grpcMaxQueryBatch   = 1 << 16
	grpcMaxDim          = 1 << 14
	grpcMaxTopK         = 1 << 16
)

func validateFlatShape(n, dim, maxN int) error {
	if n <= 0 || n > maxN {
		return fmt.Errorf("invalid n=%d", n)
	}
	if dim <= 0 || dim > grpcMaxDim {
		return fmt.Errorf("invalid dim=%d", dim)
	}
	if n > maxInt()/dim {
		return fmt.Errorf("n*dim overflows int")
	}
	return nil
}

func float32sFromLittleEndianBytes(payload []byte, n int) ([]float32, error) {
	if n < 0 || n > maxInt()/4 {
		return nil, fmt.Errorf("invalid float32 count %d", n)
	}
	if len(payload) != n*4 {
		return nil, fmt.Errorf("byte length %d must equal %d", len(payload), n*4)
	}
	if n == 0 {
		return nil, nil
	}
	values := make([]float32, n)
	raw := unsafe.Slice((*byte)(unsafe.Pointer(&values[0])), n*4)
	copy(raw, payload)
	return values, nil
}

func float32sToLittleEndianBytes(values []float32) []byte {
	if len(values) == 0 {
		return nil
	}
	out := make([]byte, len(values)*4)
	raw := unsafe.Slice((*byte)(unsafe.Pointer(&values[0])), len(values)*4)
	copy(out, raw)
	return out
}

func int64sToLittleEndianBytes(values []int64) []byte {
	if len(values) == 0 {
		return nil
	}
	out := make([]byte, len(values)*8)
	raw := unsafe.Slice((*byte)(unsafe.Pointer(&values[0])), len(values)*8)
	copy(out, raw)
	return out
}

func maxInt() int {
	return int(^uint(0) >> 1)
}
