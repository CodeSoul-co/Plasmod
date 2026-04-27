//go:build test
// +build test

package balance

import (
	"github.com/milvus-io/milvus/internal/streamingcoord/server/balancer"
	"plasmod/platformpkg/pkg/util/syncutil"
)

func ResetBalancer() {
	singleton = syncutil.NewFuture[balancer.Balancer]()
}
