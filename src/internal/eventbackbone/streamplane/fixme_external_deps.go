//go:build extended
// +build extended

// Package streamplane wires the distributed stream plane into CogDB.
//
// # FIXME: Unresolved External Dependencies
//
// The following import paths reference github.com/milvus-io/milvus/internal/...
// which lives in the upstream Milvus monorepo and is NOT present in this
// repository.  They must be resolved before this sub-module can be compiled
// with the `extended` build tag:
//
//   - github.com/milvus-io/milvus/internal/allocator
//   - github.com/milvus-io/milvus/internal/compaction
//   - github.com/milvus-io/milvus/internal/coordinator/snmanager
//   - github.com/milvus-io/milvus/internal/distributed/streaming
//   - github.com/milvus-io/milvus/internal/flushcommon/broker
//   - github.com/milvus-io/milvus/internal/flushcommon/io
//   - github.com/milvus-io/milvus/internal/flushcommon/metacache
//   - github.com/milvus-io/milvus/internal/flushcommon/metacache/pkoracle
//   - github.com/milvus-io/milvus/internal/flushcommon/pipeline
//   - github.com/milvus-io/milvus/internal/flushcommon/syncmgr
//   - github.com/milvus-io/milvus/internal/flushcommon/util
//   - github.com/milvus-io/milvus/internal/flushcommon/writebuffer
//   - github.com/milvus-io/milvus/internal/proto (generated)
//   - github.com/milvus-io/milvus/internal/storage
//   - github.com/milvus-io/milvus/internal/streamingcoord/server/balancer/policy
//   - github.com/milvus-io/milvus/internal/util/dependency
//
// Resolution options:
//  1. Vendor the required packages from the upstream Milvus monorepo into
//     this sub-tree and rename import paths to andb/streamplane/vendor/...
//  2. Replace with CogDB-native equivalents implementing the same interfaces.
//  3. Remove files that depend on unavailable packages and stub the contracts.
package streamplane
