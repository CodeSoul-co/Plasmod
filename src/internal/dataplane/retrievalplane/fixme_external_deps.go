//go:build extended
// +build extended

// Package retrievalplane wires the C++ retrieval engine into CogDB.
//
// # FIXME: Unresolved External Dependencies
//
// The following import paths reference github.com/milvus-io/milvus/internal/...
// which lives in the upstream Milvus monorepo and is NOT present in this
// repository.  They must be resolved before this sub-module can be compiled
// with the `extended` build tag:
//
//   - github.com/milvus-io/milvus/internal/agg
//   - github.com/milvus-io/milvus/internal/allocator
//   - github.com/milvus-io/milvus/internal/distributed/querynode/client
//   - github.com/milvus-io/milvus/internal/distributed/streaming
//   - github.com/milvus-io/milvus/internal/json
//   - github.com/milvus-io/milvus/internal/mocks
//   - github.com/milvus-io/milvus/internal/mocks/distributed/mock_streaming
//   - github.com/milvus-io/milvus/internal/mocks/flushcommon/mock_util
//   - github.com/milvus-io/milvus/internal/proto (generated)
//   - github.com/milvus-io/milvus/internal/storage
//   - github.com/milvus-io/milvus/internal/storage/compress
//   - github.com/milvus-io/milvus/internal/util/cgo
//   - github.com/milvus-io/milvus/internal/util/dependency
//   - github.com/milvus-io/milvus/internal/util/symbolizer
//
// Resolution options:
//  1. Vendor the required packages from the upstream Milvus monorepo into
//     this sub-tree and rename import paths to andb/retrievalplane/vendor/...
//  2. Replace with CogDB-native equivalents implementing the same interfaces.
//  3. Remove files that depend on unavailable packages and stub the contracts.
package retrievalplane
