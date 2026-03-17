//go:build test
// +build test

package broadcast

import (
	"github.com/milvus-io/milvus/internal/streamingcoord/server/broadcaster"
	"andb/platformpkg/pkg/util/syncutil"
)

func ResetBroadcaster() {
	Release()
	singleton = syncutil.NewFuture[broadcaster.Broadcaster]()
}
