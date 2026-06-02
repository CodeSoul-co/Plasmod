package grpcapi

import (
	"fmt"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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
