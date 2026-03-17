//go:build extended
// +build extended

package resource

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/milvus-io/milvus/internal/mocks/mock_metastore"
	"github.com/milvus-io/milvus/internal/mocks/mock_storage"
	"github.com/milvus-io/milvus/internal/types"
	"andb/platformpkg/pkg/util/paramtable"
	"andb/platformpkg/pkg/util/syncutil"
)

func TestMain(m *testing.M) {
	paramtable.Init()
	os.Exit(m.Run())
}

func TestInit(t *testing.T) {
	assert.Panics(t, func() {
		Init(OptETCD(&clientv3.Client{}),
			OptMixCoordClient(syncutil.NewFuture[types.MixCoordClient]()))
	})

	Init(
		OptChunkManager(mock_storage.NewMockChunkManager(t)),
		OptETCD(&clientv3.Client{}),
		OptMixCoordClient(syncutil.NewFuture[types.MixCoordClient]()),
		OptStreamingNodeCatalog(mock_metastore.NewMockStreamingNodeCataLog(t)),
	)
	assert.NotNil(t, Resource().TSOAllocator())
	assert.NotNil(t, Resource().ETCD())
	assert.NotNil(t, Resource().MixCoordClient())
	Release()
}

func TestInitForTest(t *testing.T) {
	InitForTest(t)
}
