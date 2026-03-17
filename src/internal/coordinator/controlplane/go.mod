module andb/controlplane

// TODO(fixme): This sub-module imports github.com/milvus-io/milvus/internal/...
// packages that are NOT present in this repository (upstream Milvus monorepo).
// Building with the `extended` tag will fail until those packages are either
// vendored, replaced with CogDB-native implementations, or stubbed out.
// Tracked in: src/internal/coordinator/controlplane/fixme_external_deps.go

go 1.22
