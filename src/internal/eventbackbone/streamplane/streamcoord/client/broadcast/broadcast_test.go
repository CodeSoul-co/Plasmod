//go:build extended
// +build extended

package broadcast

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/milvus-io/milvus-proto/go-api/v2/msgpb"
	"github.com/milvus-io/milvus/internal/mocks/util/streamingutil/service/mock_lazygrpc"
	"github.com/milvus-io/milvus/internal/util/streamingutil/service/lazygrpc"
	"andb/platformpkg/pkg/mocks/proto/mock_streamingpb"
	"andb/platformpkg/pkg/proto/streamingpb"
	"andb/platformpkg/pkg/streaming/util/message"
	"andb/platformpkg/pkg/streaming/walimpls/impls/rmq"
	"andb/platformpkg/pkg/streaming/walimpls/impls/walimplstest"
)

func TestBroadcast(t *testing.T) {
	s := newMockServer(t, 0)
	bs := NewGRPCBroadcastService(s)
	msg := message.NewDropCollectionMessageBuilderV1().
		WithHeader(&message.DropCollectionMessageHeader{}).
		WithBody(&msgpb.DropCollectionRequest{}).
		WithBroadcast([]string{"v1"}).
		MustBuildBroadcast()
	_, err := bs.Broadcast(context.Background(), msg)
	assert.NoError(t, err)
	msg1 := msg.WithBroadcastID(1).SplitIntoMutableMessage()
	immutableMsg1 := msg1[0].IntoImmutableMessage(rmq.NewRmqID(1))
	err = bs.Ack(context.Background(), immutableMsg1)
	assert.NoError(t, err)
}

func newMockServer(t *testing.T, sendDelay time.Duration) lazygrpc.Service[streamingpb.StreamingCoordBroadcastServiceClient] {
	s := mock_lazygrpc.NewMockService[streamingpb.StreamingCoordBroadcastServiceClient](t)
	c := mock_streamingpb.NewMockStreamingCoordBroadcastServiceClient(t)
	s.EXPECT().GetService(mock.Anything).Return(c, nil)
	c.EXPECT().Broadcast(mock.Anything, mock.Anything).Return(&streamingpb.BroadcastResponse{
		Results: map[string]*streamingpb.ProduceMessageResponseResult{
			"v1": {
				Id: walimplstest.NewTestMessageID(1).IntoProto(),
			},
		},
		BroadcastId: 1,
	}, nil).Maybe()
	c.EXPECT().Ack(mock.Anything, mock.Anything).Return(&streamingpb.BroadcastAckResponse{}, nil).Maybe()
	return s
}
