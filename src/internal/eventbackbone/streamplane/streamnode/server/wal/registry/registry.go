//go:build extended
// +build extended

package registry

import (
	"github.com/milvus-io/milvus/internal/streamingnode/server/wal"
	"github.com/milvus-io/milvus/internal/streamingnode/server/wal/adaptor"
	"github.com/milvus-io/milvus/internal/streamingnode/server/wal/interceptors"
	"andb/platformpkg/pkg/streaming/util/message"
	"andb/platformpkg/pkg/streaming/walimpls/registry"
)

// MustGetBuilder returns the wal builder by name.
// Deprecated: Use NewOpener instead.
func MustGetBuilder(name message.WALName, interceptorBuilders ...interceptors.InterceptorBuilder) wal.OpenerBuilder {
	b := registry.MustGetBuilder(name)
	return adaptor.AdaptImplsToBuilder(b, interceptorBuilders...)
}
