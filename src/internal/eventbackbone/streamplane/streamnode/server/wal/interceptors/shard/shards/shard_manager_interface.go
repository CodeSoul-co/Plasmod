//go:build extended
// +build extended

package shards

import (
	"github.com/milvus-io/milvus/internal/streamingnode/server/wal/interceptors/shard/utils"
	"plasmod/platformpkg/pkg/log"
	"plasmod/platformpkg/pkg/streaming/util/message"
	"plasmod/platformpkg/pkg/streaming/util/types"
)

type ShardManager interface {
	log.WithLogger

	Channel() types.PChannelInfo

	CheckIfCollectionCanBeCreated(collectionID int64) error

	CheckIfCollectionExists(collectionID int64) error

	CreateCollection(msg message.ImmutableCreateCollectionMessageV1)

	DropCollection(msg message.ImmutableDropCollectionMessageV1)

	CheckIfPartitionCanBeCreated(uniquePartitionKey PartitionUniqueKey) error

	CheckIfPartitionExists(uniquePartitionKey PartitionUniqueKey) error

	CreatePartition(msg message.ImmutableCreatePartitionMessageV1)

	DropPartition(msg message.ImmutableDropPartitionMessageV1)

	CheckIfSegmentCanBeCreated(uniquePartitionKey PartitionUniqueKey, segmentID int64) error

	CheckIfSegmentCanBeFlushed(uniquePartitionKey PartitionUniqueKey, segmentID int64) error

	CreateSegment(msg message.ImmutableCreateSegmentMessageV2)

	FlushSegment(msg message.ImmutableFlushMessageV2)

	AssignSegment(req *AssignSegmentRequest) (*AssignSegmentResult, error)

	ApplyDelete(msg message.MutableDeleteMessageV1) error

	WaitUntilGrowingSegmentReady(uniquePartitionKey PartitionUniqueKey) (<-chan struct{}, error)

	FlushAndFenceSegmentAllocUntil(collectionID int64, timetick uint64) ([]int64, error)

	FlushAllAndFenceSegmentAllocUntil(timetick uint64) ([]int64, error)

	AsyncFlushSegment(signal utils.SealSegmentSignal)

	Close()
}
