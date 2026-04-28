// Package modelservicepb contains protobuf bindings for the Milvus
// model_service.proto definitions (TextEmbeddingService / RerankService
// / HighlightService).
//
// DEPRECATED — NOT WIRED INTO PLASMOD RUNTIME.
//
// These RPC stubs target a Python-side model server (sentence-transformers,
// cross-encoder, etc.) that Milvus operates out-of-process via gRPC.  No
// production code in src/ imports this package: the plain text below is
// what `grep -rln modelservicepb src/` returns at the time of writing —
//
//	src/internal/platformpkg/pkg/proto/model_service.proto
//	src/internal/platformpkg/pkg/proto/modelservicepb/model_service.pb.go
//	src/internal/platformpkg/pkg/proto/modelservicepb/model_service_grpc.pb.go
//	src/internal/platformpkg/pkg/proto/modelservicepb/doc.go (this file)
//
// — i.e. only the proto definitions and the generated bindings themselves.
//
// The live embedding and retrieval paths in Plasmod are:
//
//   - Embedding generation:  src/internal/dataplane/embedding/embedding.go
//     (TfidfEmbedder pure-Go default; HTTPEmbedder for OpenAI / ZhipuAI /
//     Cohere REST; GGUF via go-llama.cpp libbinding.a — none of which use
//     Python at runtime).
//
//   - Vector retrieval (HNSW / IVF_FLAT / DiskANN):
//     src/internal/dataplane/retrievalplane/bridge.go
//     (CGO into libplasmod_retrieval.so → C++ Knowhere/FAISS).
//
//   - Sparse retrieval (SPARSE_INVERTED_INDEX / SPARSE_WAND, BM25-style):
//     src/internal/dataplane/retrievalplane/sparse_bridge.go
//     (CGO into the same shared library).
//
//   - Hybrid fusion + reranking:  src/internal/retrieval/retriever.go
//     (RRF over dense+sparse+lexical channels, pure Go).
//
// This package is kept only so an automated re-sync from upstream
// milvus-io/milvus's pkg/proto tree does not produce a confusing
// structural diff.  Do not import it from new code; use the embedding
// or retrievalplane packages above instead.
package modelservicepb
