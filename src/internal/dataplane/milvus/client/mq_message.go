package client

import (
	"context"

	"andb/src/internal/dataplane/milvus/proto/milvuspb"
	"andb/src/internal/dataplane/milvus/proto/msgpb"
	"andb/src/internal/dataplane/milvus/entity"
)

func (c *GrpcClient) ReplicateMessage(ctx context.Context,
	channelName string, beginTs, endTs uint64,
	msgsBytes [][]byte, startPositions, endPositions []*msgpb.MsgPosition,
	opts ...ReplicateMessageOption) (*entity.MessageInfo, error) {

	if c.Service == nil {
		return nil, ErrClientNotReady
	}
	req := &milvuspb.ReplicateMessageRequest{
		ChannelName:    channelName,
		BeginTs:        beginTs,
		EndTs:          endTs,
		Msgs:           msgsBytes,
		StartPositions: startPositions,
		EndPositions:   endPositions,
	}
	for _, opt := range opts {
		opt(req)
	}
	resp, err := c.Service.ReplicateMessage(ctx, req)
	if err != nil {
		return nil, err
	}
	err = handleRespStatus(resp.GetStatus())
	if err != nil {
		return nil, err
	}
	return &entity.MessageInfo{
		Position: resp.GetPosition(),
	}, nil
}
