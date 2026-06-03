package grpcapi

import (
	"encoding/binary"
	"fmt"
	"math"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	plasmodv1 "plasmod/src/internal/api/grpc/pb/plasmod/v1"
)

func TestMapServiceErrorUnavailable(t *testing.T) {
	err := mapServiceError(fmt.Errorf("too many concurrent writes; try again later"))
	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("expected gRPC status error")
	}
	if st.Code() != codes.Unavailable {
		t.Fatalf("code=%v want Unavailable", st.Code())
	}
}

func TestMapServiceErrorInvalidArgument(t *testing.T) {
	err := mapServiceError(fmt.Errorf("vectors is required"))
	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("expected gRPC status error")
	}
	if st.Code() != codes.InvalidArgument {
		t.Fatalf("code=%v want InvalidArgument", st.Code())
	}
}

func TestFlatWireFloat32RoundTripLittleEndian(t *testing.T) {
	values := []float32{1, -2.5, 3.25}
	payload := float32sToLittleEndianBytes(values)
	if got, want := len(payload), len(values)*4; got != want {
		t.Fatalf("payload len=%d want %d", got, want)
	}
	if got := binary.LittleEndian.Uint32(payload[:4]); got != math.Float32bits(values[0]) {
		t.Fatalf("first bits=%x want %x", got, math.Float32bits(values[0]))
	}
	roundTrip, err := float32sFromLittleEndianBytes(payload, len(values))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	for i := range values {
		if roundTrip[i] != values[i] {
			t.Fatalf("value[%d]=%v want %v", i, roundTrip[i], values[i])
		}
	}
}

func TestFlatWireInt64LittleEndian(t *testing.T) {
	values := []int64{1, -1, 42}
	payload := int64sToLittleEndianBytes(values)
	if got, want := len(payload), len(values)*8; got != want {
		t.Fatalf("payload len=%d want %d", got, want)
	}
	if got := int64(binary.LittleEndian.Uint64(payload[8:16])); got != -1 {
		t.Fatalf("second value=%d want -1", got)
	}
}

func TestFlatWireRejectsWrongLength(t *testing.T) {
	_, err := float32sFromLittleEndianBytes([]byte{0, 0, 0}, 1)
	if err == nil {
		t.Fatal("expected length error")
	}
}

func TestServiceDescriptorRegistersFlatDataplaneRPCs(t *testing.T) {
	want := map[string]bool{
		"IngestVectorsFlat": false,
		"QueryBatchFlat":    false,
	}
	for _, method := range plasmodv1.PlasmodAPIService_ServiceDesc.Methods {
		if _, ok := want[method.MethodName]; ok {
			want[method.MethodName] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Fatalf("service descriptor missing %s", name)
		}
	}
}
