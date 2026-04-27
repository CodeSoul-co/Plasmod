//go:build extended
// +build extended

package balance

import (
	"context"

	"github.com/milvus-io/milvus/internal/streamingcoord/server/balancer"
	"plasmod/platformpkg/pkg/util/syncutil"
)

var singleton = syncutil.NewFuture[balancer.Balancer]()

func Register(balancer balancer.Balancer) {
	singleton.Set(balancer)
}

func SetFileResourceChecker(checker balancer.FileResourceChecker) {
	singleton.Get().SetFileResourceChecker(checker)
}

func GetWithContext(ctx context.Context) (balancer.Balancer, error) {
	return singleton.GetWithContext(ctx)
}

func Release() {
	if !singleton.Ready() {
		return
	}
	singleton.Get().Close()
}
