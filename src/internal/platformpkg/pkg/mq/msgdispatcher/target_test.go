//go:build extended
// +build extended

package msgdispatcher

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"

	"github.com/milvus-io/milvus-proto/go-api/v2/msgpb"
	"andb/platformpkg/pkg/log"
	"andb/platformpkg/pkg/mq/msgstream"
	"andb/platformpkg/pkg/util/paramtable"
)

func TestSendTimeout(t *testing.T) {
	target := newTarget(&StreamConfig{
		VChannel: "test1",
		Pos:      &msgpb.MsgPosition{},
	}, false)

	time.Sleep(paramtable.Get().MQCfg.MaxTolerantLag.GetAsDuration(time.Second))

	counter := 0
	for i := 0; i < 10; i++ {
		err := target.send(&msgstream.MsgPack{})
		if err != nil {
			log.Error("send failed", zap.Int("idx", i), zap.Error(err))
			counter++
		}
	}
	assert.Equal(t, counter, 0)
}

func TestSendTimeTickFiltering(t *testing.T) {
	target := newTarget(&StreamConfig{
		VChannel: "test1",
		Pos:      &msgpb.MsgPosition{},
	}, true)
	target.send(&msgstream.MsgPack{
		EndPositions: []*msgpb.MsgPosition{
			{
				Timestamp: 1,
			},
		},
	})

	target.send(&msgstream.MsgPack{
		EndPositions: []*msgpb.MsgPosition{
			{
				Timestamp: 1,
			},
		},
	})
}
