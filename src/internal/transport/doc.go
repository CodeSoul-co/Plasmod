// Package transport implements high-throughput internal communication channels
// that complement the public HTTP/REST API in package access.
//
// Design layers (per project communication architecture):
//
//   - External: HTTP/REST and HTTP/SSE (handled by package access)
//   - Internal modules: binary length-prefixed wire protocol over HTTP/2
//     (this package). The framing is protobuf-compatible (varint + tagged
//     fields are not used; instead a stable hand-rolled binary layout is used
//     to avoid pulling in protoc tooling) and is intended for module-to-module
//     and node-to-node calls where JSON overhead is unacceptable.
//   - Bulk data: zero-copy float32 batches transmitted as a single contiguous
//     little-endian blob (mmap-friendly, Arrow-Flight-style payload shape).
//   - Replication: Server-Sent Events stream of WAL entries for downstream
//     followers; semantically equivalent to log-streaming replication.
//
// All endpoints exposed here are mounted under /v1/internal/rpc/ and
// /v1/wal/stream by Server.RegisterRoutes.
package transport
