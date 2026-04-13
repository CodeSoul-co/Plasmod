// Package llama is a stub placeholder for builds that do not have go-llama.cpp installed.
// Replace this directory with the real go-llama.cpp source when building with the gguf tag.
package llama

// LLama is the stub type that mirrors the real go-llama.cpp API surface.
type LLama struct{}

// ModelOption is a functional option for New.
type ModelOption func(*LLama)

// New is a no-op stub constructor.
func New(_ string, _ ...ModelOption) (*LLama, error) { return nil, nil }

// Embeddings is a no-op stub (per-call option variant).
func (*LLama) Embeddings(_ string, _ ...PredictOption) ([]float32, error) { return nil, nil }

// Free is a no-op stub.
func (*LLama) Free() {}

// PredictOption is a functional option for per-inference calls.
type PredictOption func(*PredictOptions)

// PredictOptions carries inference-time parameters.
type PredictOptions struct{}

// EnableEmbeddings is a stub ModelOption.
var EnableEmbeddings ModelOption = func(*LLama) {}

// SetContext is a stub ModelOption.
func SetContext(_ int) ModelOption { return func(*LLama) {} }

// SetGPULayers is a stub ModelOption.
func SetGPULayers(_ int) ModelOption { return func(*LLama) {} }

// SetThreads is a stub PredictOption.
func SetThreads(_ int) PredictOption { return func(*PredictOptions) {} }
