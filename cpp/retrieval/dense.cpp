// Copyright 2024 CogDB Authors
// SPDX-License-Identifier: Apache-2.0
//
// Dense retrieval — backed by CogDB-internal Knowhere (cpp/knowhere/).
// Knowhere is source-level integrated: no external git clone or FetchContent.
//
// GPU path (ANDB_WITH_GPU=1, Linux + CUDA):
//   NVIDIA RAFT — "GPU_CAGRA" (HNSW-equivalent on GPU, NVIDIA nv-tabular/raft 44.00.00).
//   Falls back to CPU "HNSW" automatically at runtime if no CUDA device is found.
//
// CPU path (default, ANDB_WITH_GPU=0):
//   hnswlib HNSW — always used, no CUDA required.
//
// Internal engines (cpp/vendor/):
//   engines/hnsw_engine/   ← hnswlib HNSW (CPU, MIT)
//   engines/faiss_engine/  ← Meta faiss slim (CPU, MIT)
//   engines/diskann_engine/ ← Microsoft DiskANN (opt-in)
//   src/index/gpu_raft/    ← NVIDIA RAFT wrappers: CAGRA/brute_force/ivf_flat/ivf_pq (opt-in)

#include "plasmod/dense.h"

// Knowhere public API
#include "knowhere/index/index_factory.h"
#include "knowhere/dataset.h"
#include "knowhere/config.h"
#include "knowhere/comp/thread_pool.h"
#include "knowhere/comp/index_param.h"
#include "knowhere/comp/local_file_manager.h"
#include "knowhere/object.h"
#include "knowhere/binaryset.h"

#ifdef ANDB_WITH_GPU
#include <cuda_runtime_api.h>
#endif

#include <algorithm>
#include <cassert>
#include <cerrno>
#include <cstdio>
#include <cstring>
#include <fstream>
#include <mutex>
#include <shared_mutex>
#include <stdexcept>
#include <vector>

namespace plasmod {

// ── Default index config ─────────────────────────────────────────────────────
static constexpr int kDefaultM              = 16;
static constexpr int kDefaultEfConstruction = 256;
static constexpr int kDefaultEfSearch        = 64;

// ── GPU detection (runtime) ──────────────────────────────────────────────────
// Returns true if at least one CUDA GPU is visible to the driver.
// Called at Init() time so the GPU check is a one-time cost, not per-search.
static bool cudaAvailable() {
#ifdef ANDB_WITH_GPU
    int count = 0;
    if (cudaGetDeviceCount(&count) != cudaSuccess) return false;
    return count > 0;
#else
    return false;
#endif
}

// ── tryCreateGpuIndex ─────────────────────────────────────────────────────────
// Attempts to create a GPU RAFT index via Knowhere's IndexFactory.
// Returns nullptr if the GPU path is unavailable or the factory call fails.
static std::unique_ptr<knowhere::Index<knowhere::IndexNode>>
tryCreateGpuIndex(const IndexConfig& cfg, const knowhere::Json& cfg_obj) {
#ifdef ANDB_WITH_GPU
    if (!cudaAvailable()) return nullptr;

    // GPU_CAGRA: CUDA-accelerated HNSW-equivalent via NVIDIA RAFT.
    // Falls back gracefully at the factory level if CUDA is absent at runtime.
    auto result = knowhere::IndexFactory::Instance().Create<float>(
        "GPU_CAGRA", knowhere::Version::GetCurrentVersion().VersionNumber());
    if (!result.has_value()) {
        // GPU not usable (no CUDA device or RAFT not compiled in) — fall through.
        return nullptr;
    }
    return std::make_unique<knowhere::Index<knowhere::IndexNode>>(std::move(result.value()));
#else
    (void)cfg; (void)cfg_obj;
    return nullptr;
#endif
}

// ── DiskANN helpers ──────────────────────────────────────────────────────────
// DiskANN's binary format = int32 nrows, int32 ncols, then nrows*ncols of T.
// The same layout as the .fbin format we use elsewhere in CogDB.
static bool WriteDiskANNBinFile(const std::string& path,
                                const float* vecs,
                                int64_t n,
                                int32_t dim) {
    std::ofstream os(path, std::ios::binary | std::ios::trunc);
    if (!os) return false;
    int32_t n32 = static_cast<int32_t>(n);
    os.write(reinterpret_cast<const char*>(&n32),  sizeof(int32_t));
    os.write(reinterpret_cast<const char*>(&dim),  sizeof(int32_t));
    os.write(reinterpret_cast<const char*>(vecs),
             static_cast<std::streamsize>(n) * dim * sizeof(float));
    return os.good();
}

// DiskANN refuses to overwrite an index at an existing prefix.  Delete any
// stale artefacts so a re-Init+Build cycle on the same prefix succeeds.
// Listed names mirror diskann::GetNecessaryFilenames + GetOptionalFilenames.
static void CleanDiskANNFiles(const std::string& prefix) {
    static const char* kSuffixes[] = {
        "_disk.index",      "_pq_pivots.bin",  "_pq_compressed.bin",
        "_sample_data.bin", "_sample_ids.bin", "_centroids.bin",
        "_max_base_norm.bin", "_disk.index_medoids.bin",
        "_disk.index_centroids.bin",  "_disk.index_pq_pivots.bin",
        "_pq_pivots.bin_centroid.bin","_pq_pivots.bin_chunk_offsets.bin",
        "_pq_pivots.bin_rearrangement_perm.bin",
    };
    for (const char* s : kSuffixes) {
        std::string p = prefix + s;
        std::remove(p.c_str());
    }
}

// ── HNSWIndexWrapper ─────────────────────────────────────────────────────────
// Wraps a single Knowhere HNSW index (CPU fallback, always available).
//
// Performance note: search_cfg_ and query_ds_ are pre-allocated member variables
// updated in-place on each Search() call. This eliminates the ~5 ms per-call
// overhead from JSON deep-copy and heap allocation that dominated nq=1 latency.
class HNSWIndexWrapper {
  public:
    HNSWIndexWrapper() = default;
    ~HNSWIndexWrapper() = default;

    bool Init(const IndexConfig& cfg) {
        cfg_ = cfg;
        if (cfg_.dim <= 0) return false;

// Resolve index type. Empty string defaults to HNSW for backward
        // compatibility with the historical "plasmod_retriever_init(HNSW, ...)"
        // invocation pattern.
        index_type_ = cfg_.index_type.empty() ? std::string("HNSW") : cfg_.index_type;

        // Common configuration shared by HNSW / IVF_FLAT.
        std::string metric = cfg_.metric_type.empty() ? "IP" : cfg_.metric_type;
        build_cfg_[knowhere::meta::METRIC_TYPE] = metric;
        build_cfg_[knowhere::meta::DIM]         = cfg_.dim;
        build_cfg_[knowhere::meta::TOPK]        = kDefaultEfSearch;

        if (index_type_ == "HNSW") {
            int M   = cfg_.hnsw_m > 0              ? cfg_.hnsw_m              : kDefaultM;
            int efC = cfg_.hnsw_ef_construction > 0 ? cfg_.hnsw_ef_construction : kDefaultEfConstruction;
            build_cfg_[knowhere::indexparam::M]             = M;
            build_cfg_[knowhere::indexparam::EFCONSTRUCTION] = efC;
        } else if (index_type_ == "IVF_FLAT") {
            // IVF clustering: NLIST = #coarse centroids built at index time;
            // NPROBE = #lists probed at query time (set in Search()).
            int nlist = cfg_.ivf_nlist > 0 ? cfg_.ivf_nlist : 128;
            build_cfg_[knowhere::indexparam::NLIST] = nlist;
        } else if (index_type_ == "DISKANN") {
            // DiskANN is on-disk: it owns a graph file + PQ shards + metadata
            // at `index_prefix` and reads the raw vectors from `data_path`
            // (DiskANN's binary format = int32 nrows, int32 ncols, then
            //  nrows*ncols floats — identical to the .fbin format we use).
            //
            // The constructor asserts the Object parameter is exactly
            // `Pack<std::shared_ptr<FileManager>>`.  We use a LocalFileManager
            // (a no-op stub: LoadFile=true, AddFile records names) because in
            // our embedded-process scenario DiskANN reads files directly via
            // LinuxAlignedFileReader; the manager just rubber-stamps the API.
            if (cfg_.diskann_index_prefix.empty()) {
                std::fprintf(stderr,
                    "plasmod: DISKANN requires non-empty diskann_index_prefix\n");
                return false;
            }
            if (metric != "L2" && metric != "IP" && metric != "COSINE") {
                std::fprintf(stderr,
                    "plasmod: DISKANN only supports L2|IP|COSINE, got %s\n",
                    metric.c_str());
                return false;
            }
            file_manager_ = std::make_shared<knowhere::LocalFileManager>();
            knowhere::Pack<std::shared_ptr<knowhere::FileManager>> pack(file_manager_);
            auto disk_result = knowhere::IndexFactory::Instance().Create<float>(
                index_type_,
                knowhere::Version::GetCurrentVersion().VersionNumber(),
                pack);
            if (!disk_result.has_value()) return false;
            index_ = std::make_unique<knowhere::Index<knowhere::IndexNode>>(
                std::move(disk_result.value()));
            dim_ = cfg_.dim;

            // Build-time params; vec_field_size_gb is finalized in Build()
            // once we know n.  See diskann_config.h for parameter semantics.
            const std::string data_path = cfg_.diskann_index_prefix + ".raw_data.bin";
            build_cfg_["data_path"]        = data_path;
            build_cfg_["index_prefix"]     = cfg_.diskann_index_prefix;
            build_cfg_["max_degree"]       = cfg_.diskann_max_degree   > 0
                                              ? cfg_.diskann_max_degree   : 48;
            build_cfg_["search_list_size"] = cfg_.diskann_search_list  > 0
                                              ? cfg_.diskann_search_list : 128;
            // pq/dram budgets: 0 means "auto-pick a default at Build()".
            build_cfg_["pq_code_budget_gb"]    = cfg_.diskann_pq_code_budget_gb;
            build_cfg_["build_dram_budget_gb"] = cfg_.diskann_build_dram_budget_gb;
            build_cfg_["disk_pq_dims"]         = 0;     // store uncompressed → 100% recall
            build_cfg_["accelerate_build"]     = false;
            build_cfg_["shuffle_build"]        = false;
            // Deserialize-time params:
            build_cfg_["search_cache_budget_gb"] = 0.0f; // no cache (small dataset)
            build_cfg_["warm_up"]                = false;
            build_cfg_["use_bfs_cache"]          = false;
            return true;
        } else {
            // Unknown index type — reject early with a clear log line.
            std::fprintf(stderr,
                "plasmod: unsupported dense index_type=%s (HNSW|IVF_FLAT|DISKANN)\n",
                index_type_.c_str());
            return false;
        }

        auto result = knowhere::IndexFactory::Instance().Create<float>(
            index_type_, knowhere::Version::GetCurrentVersion().VersionNumber());
        if (!result.has_value()) return false;
        index_ = std::make_unique<knowhere::Index<knowhere::IndexNode>>(
            std::move(result.value()));
        dim_ = cfg_.dim;
        return true;
    }

    bool Build(const float* vectors, int64_t n) {
        if (!index_ || !vectors || n <= 0) return false;
        std::unique_lock<std::shared_mutex> lk(mu_);

        if (index_type_ == "DISKANN") {
            // 1. DiskANN reads the raw vectors from a file on disk; write them
            //    out in DiskANN's bin format (int32 npts, int32 ncols, then
            //    raw float32 row-major).
            const std::string data_path =
                build_cfg_["data_path"].get<std::string>();
            const std::string index_prefix =
                build_cfg_["index_prefix"].get<std::string>();
            if (!WriteDiskANNBinFile(data_path, vectors, n, dim_)) {
                std::fprintf(stderr,
                    "plasmod: DISKANN failed to write %s\n", data_path.c_str());
                return false;
            }

            // 2. Finalize size-dependent build params.  vec_field_size_gb is
            //    used by DiskANNConfig::CheckAndAdjust to derive PQ/cache
            //    budgets when the *_ratio variants are non-zero.
            const float bytes      = static_cast<float>(n) * dim_ * sizeof(float);
            const float gb         = bytes / static_cast<float>(1ULL << 30);
            build_cfg_["vec_field_size_gb"] = gb;
            // Auto-pick budgets if caller left them at 0.
            if (build_cfg_["pq_code_budget_gb"].get<float>() <= 0.0f) {
                // Tiny lower bound (1 MB) so DiskANN doesn't reject as
                // "too small"; for big datasets the caller should tune.
                build_cfg_["pq_code_budget_gb"] = std::max(gb * 0.125f, 0.001f);
            }
            if (build_cfg_["build_dram_budget_gb"].get<float>() <= 0.0f) {
                // 4× raw size, with a 1 GB floor.
                build_cfg_["build_dram_budget_gb"] = std::max(gb * 4.0f, 1.0f);
            }

            // 3. DiskANN refuses to clobber pre-existing index files at the
            //    prefix; remove any leftover from a prior run.
            CleanDiskANNFiles(index_prefix);

            // 4. Build (writes the on-disk graph + PQ shards).
            auto ds     = knowhere::GenDataSet(n, dim_, vectors);  // ignored
            auto status = index_->Build(ds, build_cfg_);
            if (status != knowhere::Status::success) return false;

            // 5. After Build the engine is NOT yet query-ready
            //    (`is_prepared_=false`).  Serialize is a no-op for DiskANN;
            //    Deserialize loads the PQ flash index from disk and flips
            //    is_prepared_ to true.
            knowhere::BinarySet empty_binset;
            (void)index_->Serialize(empty_binset);
            knowhere::Json deser_cfg;
            deser_cfg["index_prefix"]            = index_prefix;
            deser_cfg["metric_type"]             = build_cfg_[knowhere::meta::METRIC_TYPE];
            deser_cfg["search_cache_budget_gb"]  = build_cfg_["search_cache_budget_gb"];
            deser_cfg["warm_up"]                 = false;
            deser_cfg["use_bfs_cache"]           = false;
            auto dstatus = index_->Deserialize(empty_binset, deser_cfg);
            if (dstatus != knowhere::Status::success) {
                std::fprintf(stderr,
                    "plasmod: DISKANN Deserialize after Build failed (%d)\n",
                    static_cast<int>(dstatus));
                return false;
            }
            num_vectors_ = n;
            return true;
        }

        auto ds     = knowhere::GenDataSet(n, dim_, vectors);
        auto status = index_->Build(ds, build_cfg_);
        if (status != knowhere::Status::success) return false;
        num_vectors_ = n;
        return true;
    }

    bool Add(const float* vectors, int64_t n) {
        if (!index_ || !vectors || n <= 0) return false;
        std::unique_lock<std::shared_mutex> lk(mu_);
        auto ds     = knowhere::GenDataSet(n, dim_, vectors);
        auto status = index_->Add(ds, build_cfg_);
        if (status != knowhere::Status::success) return false;
        num_vectors_ += n;
        return true;
    }

    bool Search(const float* query, int64_t nq, int topk,
                const uint8_t* allow_bits, int64_t allow_count,
                int64_t* out_ids, float* out_dists) {
        if (!index_ || !query) return false;
        // Read-side: Knowhere HNSW Search is read-only; use a shared lock so
        // concurrent searches can run in parallel.  Build/Add take the
        // exclusive lock above.
        std::shared_lock<std::shared_mutex> lk(mu_);

// Per-thread search config and DataSet — eliminates the per-call
        // Json deep copy and DataSet heap allocation.  Only the fields that
        // change per call (TOPK/EF, Rows/Dim/Tensor) are updated in place.
        thread_local knowhere::Json   tls_cfg;
        thread_local bool             tls_cfg_static_init = false;
        thread_local knowhere::DataSetPtr tls_qds;
        if (!tls_cfg_static_init) {
            // Build up the static portion of the search config.
            tls_cfg[knowhere::meta::METRIC_TYPE] = build_cfg_[knowhere::meta::METRIC_TYPE];
            if (index_type_ == "HNSW") {
                tls_cfg[knowhere::indexparam::M]             = build_cfg_[knowhere::indexparam::M];
                tls_cfg[knowhere::indexparam::EFCONSTRUCTION] = build_cfg_[knowhere::indexparam::EFCONSTRUCTION];
            }
            tls_cfg_static_init = true;
        }
        tls_cfg[knowhere::meta::DIM]          = dim_;
        tls_cfg[knowhere::meta::TOPK]         = topk;
        if (index_type_ == "HNSW") {
            tls_cfg[knowhere::indexparam::EF] = std::max(topk * 2, kDefaultEfSearch);
        } else if (index_type_ == "IVF_FLAT") {
            int nprobe = cfg_.ivf_nprobe > 0 ? cfg_.ivf_nprobe : 8;
            tls_cfg[knowhere::indexparam::NPROBE] = nprobe;
        } else if (index_type_ == "DISKANN") {
            // search_list_size must be >= topk per DiskANN's own check.
            // beamwidth = number of concurrent IO requests per query.
            int lsize = std::max(topk, 16);
            int bw    = cfg_.diskann_beamwidth > 0 ? cfg_.diskann_beamwidth : 8;
            tls_cfg["search_list_size"]    = lsize;
            tls_cfg["beamwidth"]           = bw;
            tls_cfg["filter_threshold"]    = -1.0f;
            tls_cfg["index_prefix"]        = build_cfg_["index_prefix"];
        }

        if (!tls_qds) {
            tls_qds = std::make_shared<knowhere::DataSet>();
            tls_qds->SetIsOwner(false);
        }
        tls_qds->SetRows(nq);
        tls_qds->SetDim(dim_);
        tls_qds->SetTensor(query);

        knowhere::expected<knowhere::DataSetPtr> res;
        if (allow_bits && allow_count > 0) {
            knowhere::BitsetView bitset(allow_bits, allow_count);
            res = index_->Search(tls_qds, tls_cfg, bitset);
        } else {
            res = index_->Search(tls_qds, tls_cfg, knowhere::BitsetView());
        }

        if (!res.has_value()) return false;
        const int64_t total = nq * topk;
        std::copy_n(res.value()->GetIds(),      total, out_ids);
        std::copy_n(res.value()->GetDistance(), total, out_dists);
        return true;
    }

    int64_t num_vectors() const { return num_vectors_; }
    int     dim()         const { return dim_; }
    bool    is_gpu()      const { return false; }

  private:
    IndexConfig cfg_;
    std::string index_type_; // "HNSW" | "IVF_FLAT" | "DISKANN"
    int         dim_         = 0;
    int64_t     num_vectors_ = 0;
    knowhere::Json  build_cfg_;
    std::unique_ptr<knowhere::Index<knowhere::IndexNode>> index_;
    // DISKANN-only: kept alive for the lifetime of the index because the
    // engine retains a copy of the shared_ptr internally.
    std::shared_ptr<knowhere::FileManager> file_manager_;
    mutable std::shared_mutex mu_;
};

// ── GpuIndexWrapper ──────────────────────────────────────────────────────────
// Wraps a GPU RAFT index (CAGRA) obtained from tryCreateGpuIndex.
// Only instantiated when ANDB_WITH_GPU=1 and a CUDA device is visible.
class GpuIndexWrapper {
  public:
    GpuIndexWrapper(std::unique_ptr<knowhere::Index<knowhere::IndexNode>> idx,
                    const IndexConfig& cfg)
        : index_(std::move(idx)), cfg_(cfg) {
        if (index_) {
            knowhere::Json init_cfg;
            init_cfg[knowhere::meta::DIM] = cfg.dim;
            (void)init_cfg; // RAFT GPU index may not need explicit Build-time config
            dim_ = cfg.dim;
        }
    }

    bool Build(const float* vectors, int64_t n) {
        if (!index_ || !vectors || n <= 0) return false;
        std::unique_lock<std::shared_mutex> lk(mu_);
        auto ds     = knowhere::GenDataSet(n, dim_, vectors);
        knowhere::Json build_cfg;
        build_cfg[knowhere::meta::DIM]    = dim_;
        build_cfg[knowhere::meta::TOPK]   = kDefaultEfSearch;
        auto status = index_->Build(ds, build_cfg);
        if (status != knowhere::Status::success) return false;
        num_vectors_ = n;
        return true;
    }

    bool Add(const float* vectors, int64_t n) {
        if (!index_ || !vectors || n <= 0) return false;
        std::unique_lock<std::shared_mutex> lk(mu_);
        auto ds     = knowhere::GenDataSet(n, dim_, vectors);
        auto status = index_->Add(ds, knowhere::Json{});
        if (status != knowhere::Status::success) return false;
        num_vectors_ += n;
        return true;
    }

    bool Search(const float* query, int64_t nq, int topk,
                const uint8_t* allow_bits, int64_t allow_count,
                int64_t* out_ids, float* out_dists) {
        if (!index_ || !query) return false;
        std::shared_lock<std::shared_mutex> lk(mu_);
        knowhere::Json search_cfg;
        search_cfg[knowhere::meta::TOPK] = topk;
        auto qds = knowhere::GenDataSet(nq, dim_, query);
        knowhere::expected<knowhere::DataSetPtr> res;
        if (allow_bits && allow_count > 0) {
            knowhere::BitsetView bitset(allow_bits, allow_count);
            res = index_->Search(qds, search_cfg, bitset);
        } else {
            res = index_->Search(qds, search_cfg, knowhere::BitsetView());
        }
        if (!res.has_value()) return false;
        const int64_t total = nq * topk;
        std::copy_n(res.value()->GetIds(),      total, out_ids);
        std::copy_n(res.value()->GetDistance(), total, out_dists);
        return true;
    }

    int64_t num_vectors() const { return num_vectors_; }
    int     dim()         const { return dim_; }
    bool    is_gpu()      const { return true; }

  private:
    std::unique_ptr<knowhere::Index<knowhere::IndexNode>> index_;
    IndexConfig cfg_;
    int         dim_          = 0;
    int64_t     num_vectors_   = 0;
    mutable std::shared_mutex mu_;
};

// ── DenseRetrieverImpl ─────────────────────────────────────────────────────
// Dual-path retriever: GPU (CAGRA/RAFT) when available, CPU (HNSW) otherwise.
// Init() probes GPU availability once; Build/Search/Add always route to the
// selected backend without any per-call overhead.
class DenseRetrieverImpl {
  public:
    bool Init(const IndexConfig& cfg) {
        gpu_index_ = tryCreateGpuIndex(cfg, knowhere::Json{});
        if (gpu_index_) {
            gpu_wrapper_ = std::make_unique<GpuIndexWrapper>(std::move(gpu_index_), cfg);
            return true;
        }
        hnsw_wrapper_ = std::make_unique<HNSWIndexWrapper>();
        return hnsw_wrapper_->Init(cfg);
    }

    bool Build(const float* vecs, int64_t n) {
        if (gpu_wrapper_) return gpu_wrapper_->Build(vecs, n);
        return hnsw_wrapper_ && hnsw_wrapper_->Build(vecs, n);
    }

    bool Add(const float* vecs, int64_t n) {
        if (gpu_wrapper_) return gpu_wrapper_->Add(vecs, n);
        return hnsw_wrapper_ && hnsw_wrapper_->Add(vecs, n);
    }

    bool Search(const float* query, int64_t nq, int topk,
                const uint8_t* allow_bits, int64_t allow_count,
                int64_t* ids, float* dists) {
        if (gpu_wrapper_) return gpu_wrapper_->Search(query, nq, topk, allow_bits, allow_count, ids, dists);
        return hnsw_wrapper_ && hnsw_wrapper_->Search(query, nq, topk, allow_bits, allow_count, ids, dists);
    }

    int64_t Count() const {
        if (gpu_wrapper_) return gpu_wrapper_->num_vectors();
        return hnsw_wrapper_ ? hnsw_wrapper_->num_vectors() : 0;
    }
    int Dim() const {
        if (gpu_wrapper_) return gpu_wrapper_->dim();
        return hnsw_wrapper_ ? hnsw_wrapper_->dim() : 0;
    }
    bool IsGpu() const { return gpu_wrapper_ != nullptr; }

  private:
    // GPU path (non-null when CUDA available at Init time)
    std::unique_ptr<knowhere::Index<knowhere::IndexNode>> gpu_index_; // owned by wrapper
    std::unique_ptr<GpuIndexWrapper> gpu_wrapper_;
    // CPU fallback
    std::unique_ptr<HNSWIndexWrapper> hnsw_wrapper_;
};

// ── DenseRetriever public API ───────────────────────────────────────────────
DenseRetriever::DenseRetriever()  = default;
DenseRetriever::~DenseRetriever() = default;

bool DenseRetriever::Init(const IndexConfig& cfg) {
    impl_ = std::make_unique<DenseRetrieverImpl>();
    return impl_->Init(cfg);
}
bool DenseRetriever::Build(const float* v, int64_t n) {
    return impl_ && impl_->Build(v, n);
}
bool DenseRetriever::Add(const float* v, int64_t n) {
    return impl_ && impl_->Add(v, n);
}
bool DenseRetriever::Search(const float* q, int64_t nq, int topk,
                            const uint8_t* allow_bits, int64_t allow_count,
                            int64_t* out_ids, float* out_dists) {
    return impl_ && impl_->Search(q, nq, topk, allow_bits, allow_count,
                                  out_ids, out_dists);
}
int64_t DenseRetriever::Count() const { return impl_ ? impl_->Count() : 0; }
int     DenseRetriever::Dim()   const { return impl_ ? impl_->Dim()   : 0; }

}  // namespace plasmod
