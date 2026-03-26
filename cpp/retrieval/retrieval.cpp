// Copyright 2024 CogDB Authors
// SPDX-License-Identifier: Apache-2.0
//
// Unified retrieval implementation combining dense, sparse, and filter paths.

#include "andb/retrieval.h"
#include <chrono>
#include <cstring>

namespace andb {

static const char* kVersion = "andb-retrieval-0.2.0";

const char* Version() {
    return kVersion;
}

// Retriever implementation

Retriever::Retriever()
    : dense_(std::make_unique<DenseRetriever>()),
      sparse_(std::make_unique<SparseRetriever>()),
      merger_(std::make_unique<Merger>()) {}

Retriever::~Retriever() = default;

Retriever::Retriever(Retriever&&) noexcept = default;
Retriever& Retriever::operator=(Retriever&&) noexcept = default;

bool Retriever::Init(
    const IndexConfig& dense_config,
    const std::string& sparse_index_type,
    const MergeConfig& merge_config
) {
    if (!dense_->Init(dense_config)) {
        return false;
    }
    if (!sparse_->Init(sparse_index_type)) {
        return false;
    }
    merger_->SetConfig(merge_config);
    return true;
}

bool Retriever::Build(
    const float* dense_vectors,
    const SparseVector* sparse_vectors,
    int64_t num_vectors
) {
    if (!dense_->Build(dense_vectors, num_vectors)) {
        return false;
    }
    if (!sparse_->Build(sparse_vectors, num_vectors)) {
        return false;
    }
    ready_ = true;
    return true;
}

RetrievalResult Retriever::Retrieve(const RetrievalRequest& request) const {
    auto start = std::chrono::steady_clock::now();
    RetrievalResult result;
    
    if (!ready_) {
        return result;
    }
    
    SearchResult dense_results;
    SearchResult sparse_results;
    
    // Effective top_k for search (fetch more for better RRF merge)
    int32_t search_k = request.for_graph ? request.top_k * 4 : request.top_k * 2;
    
    // Execute dense search if enabled
    if (request.enable_dense && request.query_vector && request.vector_dim > 0) {
        dense_results.ids.resize(search_k, -1);
        dense_results.distances.resize(search_k, 0.0f);
        bool ok = dense_->Search(
            request.query_vector,
            1,
            search_k,
            request.filter_bitset,
            static_cast<int64_t>(request.filter_bitset_size * 8),
            dense_results.ids.data(),
            dense_results.distances.data()
        );
        if (ok) {
            // Trim trailing -1 sentinels
            dense_results.count = 0;
            for (int64_t i = 0; i < search_k; ++i) {
                if (dense_results.ids[i] < 0) break;
                ++dense_results.count;
            }
            dense_results.ids.resize(dense_results.count);
            dense_results.distances.resize(dense_results.count);
        }
        result.dense_hits = dense_results.count;
    }
    
    // Execute sparse search if enabled
    if (request.enable_sparse && !request.query_text.empty()) {
        SparseVector query_sparse = SparseRetriever::TextToSparse(request.query_text);
        sparse_results = sparse_->Search(
            query_sparse,
            search_k,
            request.filter_bitset,
            request.filter_bitset_size
        );
        result.sparse_hits = sparse_results.count;
    }
    
    // Merge results
    result.candidates = merger_->Merge(
        dense_results,
        sparse_results,
        request.top_k,
        request.for_graph
    );
    
    result.total_found = static_cast<int64_t>(result.candidates.size());
    
    auto end = std::chrono::steady_clock::now();
    result.latency_ms = std::chrono::duration_cast<std::chrono::milliseconds>(end - start).count();
    
    return result;
}

RetrievalResult Retriever::BenchmarkRetrieve(const RetrievalRequest& request) const {
    auto start = std::chrono::steady_clock::now();
    RetrievalResult result;
    
    if (!ready_) {
        return result;
    }
    
    SearchResult dense_results;
    SearchResult sparse_results;
    
    // For benchmark, fetch all available results
    int32_t search_k = 10000;  // Large number to get all
    
    if (request.enable_dense && request.query_vector && request.vector_dim > 0) {
        dense_results.ids.resize(search_k, -1);
        dense_results.distances.resize(search_k, 0.0f);
        bool ok = dense_->Search(
            request.query_vector,
            1,
            search_k,
            request.filter_bitset,
            static_cast<int64_t>(request.filter_bitset_size * 8),
            dense_results.ids.data(),
            dense_results.distances.data()
        );
        if (ok) {
            dense_results.count = 0;
            for (int64_t i = 0; i < search_k; ++i) {
                if (dense_results.ids[i] < 0) break;
                ++dense_results.count;
            }
            dense_results.ids.resize(dense_results.count);
            dense_results.distances.resize(dense_results.count);
        }
        result.dense_hits = dense_results.count;
    }
    
    if (request.enable_sparse && !request.query_text.empty()) {
        SparseVector query_sparse = SparseRetriever::TextToSparse(request.query_text);
        sparse_results = sparse_->Search(
            query_sparse,
            search_k,
            request.filter_bitset,
            request.filter_bitset_size
        );
        result.sparse_hits = sparse_results.count;
    }
    
    // Merge WITHOUT truncation for benchmark
    result.candidates = merger_->Merge(
        dense_results,
        sparse_results,
        search_k,  // No truncation
        false
    );
    
    result.total_found = static_cast<int64_t>(result.candidates.size());
    
    auto end = std::chrono::steady_clock::now();
    result.latency_ms = std::chrono::duration_cast<std::chrono::milliseconds>(end - start).count();
    
    return result;
}

DenseRetriever& Retriever::GetDenseRetriever() { return *dense_; }
const DenseRetriever& Retriever::GetDenseRetriever() const { return *dense_; }
SparseRetriever& Retriever::GetSparseRetriever() { return *sparse_; }
const SparseRetriever& Retriever::GetSparseRetriever() const { return *sparse_; }
Merger& Retriever::GetMerger() { return *merger_; }
const Merger& Retriever::GetMerger() const { return *merger_; }

bool Retriever::Serialize(std::vector<uint8_t>& output) const {
    // TODO: Implement serialization
    return false;
}

bool Retriever::Deserialize(const std::vector<uint8_t>& input) {
    // TODO: Implement deserialization
    return false;
}

bool Retriever::Load(const std::string& dir_path) {
    // TODO: Implement loading
    return false;
}

bool Retriever::Save(const std::string& dir_path) const {
    // TODO: Implement saving
    return false;
}

bool Retriever::IsReady() const {
    return ready_;
}

}  // namespace andb

// C API implementation

const char* andb_version() {
    return andb::Version();
}

void* andb_retriever_create() {
    return new andb::Retriever();
}

void andb_retriever_destroy(void* retriever) {
    delete static_cast<andb::Retriever*>(retriever);
}

int andb_retriever_init(
    void* retriever,
    const char* dense_index_type,
    const char* metric_type,
    int dim,
    const char* sparse_index_type,
    int rrf_k
) {
    if (!retriever) return 0;
    
    andb::IndexConfig dense_config;
    dense_config.index_type = dense_index_type ? dense_index_type : "HNSW";
    dense_config.metric_type = metric_type ? metric_type : "IP";
    dense_config.dim = dim;
    
    andb::MergeConfig merge_config;
    merge_config.rrf_k = rrf_k > 0 ? rrf_k : 60;
    
    auto* r = static_cast<andb::Retriever*>(retriever);
    return r->Init(dense_config, sparse_index_type ? sparse_index_type : "SPARSE_INVERTED_INDEX", merge_config) ? 1 : 0;
}

int andb_retriever_build(
    void* retriever,
    const float* dense_vectors,
    int64_t num_vectors,
    int dim
) {
    if (!retriever || !dense_vectors || num_vectors <= 0 || dim <= 0) return 0;
    
    // For now, build without sparse vectors (can be extended)
    auto* r = static_cast<andb::Retriever*>(retriever);
    return r->GetDenseRetriever().Build(dense_vectors, num_vectors) ? 1 : 0;
}

int andb_retriever_search(
    void* retriever,
    const float* query_vector,
    int dim,
    int top_k,
    int for_graph,
    const uint8_t* filter_bitset,
    size_t filter_size,
    int64_t* out_ids,
    float* out_scores,
    int max_results
) {
    if (!retriever || !query_vector || dim <= 0 || top_k <= 0 || !out_ids || !out_scores) {
        return 0;
    }
    
    andb::RetrievalRequest request;
    request.query_vector = query_vector;
    request.vector_dim = dim;
    request.top_k = top_k;
    request.for_graph = for_graph != 0;
    request.filter_bitset = filter_bitset;
    request.filter_bitset_size = filter_size;
    request.enable_dense = true;
    request.enable_sparse = false;  // No text query in C API
    
    auto* r = static_cast<andb::Retriever*>(retriever);
    andb::RetrievalResult result = r->Retrieve(request);
    
    int count = std::min(static_cast<int>(result.candidates.size()), max_results);
    for (int i = 0; i < count; ++i) {
        out_ids[i] = result.candidates[i].internal_id;
        out_scores[i] = result.candidates[i].final_score;
    }
    
    return count;
}
