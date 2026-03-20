// Copyright 2024 CogDB Authors
// SPDX-License-Identifier: Apache-2.0
//
// pybind11 bindings for C++ retrieval module.
// Exposes all retrieval interfaces to Python.

#include <pybind11/pybind11.h>
#include <pybind11/stl.h>
#include <pybind11/numpy.h>

#include "andb/retrieval.h"

namespace py = pybind11;

PYBIND11_MODULE(andb_retrieval, m) {
    m.doc() = "CogDB Retrieval Module - C++ implementation with Knowhere integration";
    
    // Version
    m.def("version", &andb::Version, "Get retrieval module version");
    
    // ==========================================================================
    // Types
    // ==========================================================================
    
    py::class_<andb::Candidate>(m, "Candidate")
        .def(py::init<>())
        .def_readwrite("object_id", &andb::Candidate::object_id)
        .def_readwrite("object_type", &andb::Candidate::object_type)
        .def_readwrite("dense_score", &andb::Candidate::dense_score)
        .def_readwrite("sparse_score", &andb::Candidate::sparse_score)
        .def_readwrite("rrf_score", &andb::Candidate::rrf_score)
        .def_readwrite("final_score", &andb::Candidate::final_score)
        .def_readwrite("importance", &andb::Candidate::importance)
        .def_readwrite("freshness_score", &andb::Candidate::freshness_score)
        .def_readwrite("confidence", &andb::Candidate::confidence)
        .def_readwrite("is_seed", &andb::Candidate::is_seed)
        .def_readwrite("seed_score", &andb::Candidate::seed_score)
        .def_readwrite("from_dense", &andb::Candidate::from_dense)
        .def_readwrite("from_sparse", &andb::Candidate::from_sparse)
        .def_readwrite("from_filter", &andb::Candidate::from_filter)
        .def_readwrite("internal_id", &andb::Candidate::internal_id);
    
    py::class_<andb::SearchResult>(m, "SearchResult")
        .def(py::init<>())
        .def_readwrite("ids", &andb::SearchResult::ids)
        .def_readwrite("distances", &andb::SearchResult::distances)
        .def_readwrite("count", &andb::SearchResult::count);
    
    py::class_<andb::RetrievalResult>(m, "RetrievalResult")
        .def(py::init<>())
        .def_readwrite("candidates", &andb::RetrievalResult::candidates)
        .def_readwrite("total_found", &andb::RetrievalResult::total_found)
        .def_readwrite("dense_hits", &andb::RetrievalResult::dense_hits)
        .def_readwrite("sparse_hits", &andb::RetrievalResult::sparse_hits)
        .def_readwrite("filter_hits", &andb::RetrievalResult::filter_hits)
        .def_readwrite("latency_ms", &andb::RetrievalResult::latency_ms);
    
    py::class_<andb::IndexConfig>(m, "IndexConfig")
        .def(py::init<>())
        .def_readwrite("index_type", &andb::IndexConfig::index_type)
        .def_readwrite("metric_type", &andb::IndexConfig::metric_type)
        .def_readwrite("dim", &andb::IndexConfig::dim)
        .def_readwrite("hnsw_m", &andb::IndexConfig::hnsw_m)
        .def_readwrite("hnsw_ef_construction", &andb::IndexConfig::hnsw_ef_construction)
        .def_readwrite("hnsw_ef_search", &andb::IndexConfig::hnsw_ef_search)
        .def_readwrite("ivf_nlist", &andb::IndexConfig::ivf_nlist)
        .def_readwrite("ivf_nprobe", &andb::IndexConfig::ivf_nprobe);
    
    py::class_<andb::MergeConfig>(m, "MergeConfig")
        .def(py::init<>())
        .def_readwrite("rrf_k", &andb::MergeConfig::rrf_k)
        .def_readwrite("seed_threshold", &andb::MergeConfig::seed_threshold);
    
    py::class_<andb::SparseVector>(m, "SparseVector")
        .def(py::init<>())
        .def_readwrite("indices", &andb::SparseVector::indices)
        .def_readwrite("values", &andb::SparseVector::values);
    
    // ==========================================================================
    // DenseRetriever
    // ==========================================================================
    
    py::class_<andb::DenseRetriever>(m, "DenseRetriever")
        .def(py::init<>())
        .def("init", &andb::DenseRetriever::Init)
        .def("build", [](andb::DenseRetriever& self, py::array_t<float> vectors) {
            py::buffer_info buf = vectors.request();
            if (buf.ndim != 2) {
                throw std::runtime_error("vectors must be 2D array");
            }
            int64_t num_vectors = buf.shape[0];
            return self.Build(static_cast<float*>(buf.ptr), num_vectors);
        })
        .def("search", [](const andb::DenseRetriever& self,
                          py::array_t<float> query_vectors,
                          int32_t top_k,
                          py::object filter_bitset) {
            py::buffer_info buf = query_vectors.request();
            int64_t num_queries = buf.ndim == 1 ? 1 : buf.shape[0];
            
            const uint8_t* bitset_ptr = nullptr;
            size_t bitset_size = 0;
            if (!filter_bitset.is_none()) {
                py::array_t<uint8_t> bitset = filter_bitset.cast<py::array_t<uint8_t>>();
                py::buffer_info bitset_buf = bitset.request();
                bitset_ptr = static_cast<uint8_t*>(bitset_buf.ptr);
                bitset_size = bitset_buf.size;
            }
            
            return self.Search(
                static_cast<float*>(buf.ptr),
                num_queries,
                top_k,
                bitset_ptr,
                bitset_size
            );
        }, py::arg("query_vectors"), py::arg("top_k"), py::arg("filter_bitset") = py::none())
        .def("count", &andb::DenseRetriever::Count)
        .def("dim", &andb::DenseRetriever::Dim)
        .def("type", &andb::DenseRetriever::Type)
        .def("is_ready", &andb::DenseRetriever::IsReady);
    
    // ==========================================================================
    // SparseRetriever
    // ==========================================================================
    
    py::class_<andb::SparseRetriever>(m, "SparseRetriever")
        .def(py::init<>())
        .def("init", &andb::SparseRetriever::Init, py::arg("index_type") = "SPARSE_INVERTED_INDEX")
        .def("build", [](andb::SparseRetriever& self, const std::vector<andb::SparseVector>& vectors) {
            return self.Build(vectors.data(), static_cast<int64_t>(vectors.size()));
        })
        .def("search", [](const andb::SparseRetriever& self,
                          const andb::SparseVector& query,
                          int32_t top_k,
                          py::object filter_bitset) {
            const uint8_t* bitset_ptr = nullptr;
            size_t bitset_size = 0;
            if (!filter_bitset.is_none()) {
                py::array_t<uint8_t> bitset = filter_bitset.cast<py::array_t<uint8_t>>();
                py::buffer_info bitset_buf = bitset.request();
                bitset_ptr = static_cast<uint8_t*>(bitset_buf.ptr);
                bitset_size = bitset_buf.size;
            }
            return self.Search(query, top_k, bitset_ptr, bitset_size);
        }, py::arg("query"), py::arg("top_k"), py::arg("filter_bitset") = py::none())
        .def_static("text_to_sparse", &andb::SparseRetriever::TextToSparse)
        .def("count", &andb::SparseRetriever::Count)
        .def("type", &andb::SparseRetriever::Type)
        .def("is_ready", &andb::SparseRetriever::IsReady);
    
    // ==========================================================================
    // FilterBitset
    // ==========================================================================
    
    py::class_<andb::FilterBitset>(m, "FilterBitset")
        .def(py::init<>())
        .def(py::init<size_t>())
        .def("resize", &andb::FilterBitset::Resize)
        .def("clear", &andb::FilterBitset::Clear)
        .def("set_all", &andb::FilterBitset::SetAll)
        .def("set", &andb::FilterBitset::Set)
        .def("unset", &andb::FilterBitset::Unset)
        .def("test", &andb::FilterBitset::Test)
        .def("byte_size", &andb::FilterBitset::ByteSize)
        .def("num_bits", &andb::FilterBitset::NumBits)
        .def("count_filtered", &andb::FilterBitset::CountFiltered)
        .def("filter_ratio", &andb::FilterBitset::FilterRatio)
        .def("to_numpy", [](const andb::FilterBitset& self) {
            size_t size = self.ByteSize();
            py::array_t<uint8_t> result(size);
            std::memcpy(result.mutable_data(), self.Data(), size);
            return result;
        });
    
    // ==========================================================================
    // FilterBuilder
    // ==========================================================================
    
    py::class_<andb::FilterBuilder>(m, "FilterBuilder")
        .def(py::init<>())
        .def("set_num_ids", &andb::FilterBuilder::SetNumIds)
        .def("filter_quarantined", [](andb::FilterBuilder& self, py::array_t<bool> flags) {
            self.FilterQuarantined(flags.data());
        })
        .def("filter_expired_ttl", [](andb::FilterBuilder& self, py::array_t<int64_t> ttls, int64_t current_time) {
            self.FilterExpiredTTL(ttls.data(), current_time);
        })
        .def("filter_not_yet_visible", [](andb::FilterBuilder& self, py::array_t<int64_t> times, int64_t current_time) {
            self.FilterNotYetVisible(times.data(), current_time);
        })
        .def("filter_inactive", [](andb::FilterBuilder& self, py::array_t<bool> flags) {
            self.FilterInactive(flags.data());
        })
        .def("filter_old_version", [](andb::FilterBuilder& self, py::array_t<int32_t> versions, int32_t min_version) {
            self.FilterOldVersion(versions.data(), min_version);
        })
        .def("get_bitset", &andb::FilterBuilder::GetBitset, py::return_value_policy::reference)
        .def("take_bitset", &andb::FilterBuilder::TakeBitset);
    
    // ==========================================================================
    // Merger
    // ==========================================================================
    
    py::class_<andb::Merger>(m, "Merger")
        .def(py::init<>())
        .def(py::init<const andb::MergeConfig&>())
        .def("set_config", &andb::Merger::SetConfig)
        .def("get_config", &andb::Merger::GetConfig, py::return_value_policy::reference)
        .def("merge", &andb::Merger::Merge)
        .def("compute_rrf_score", &andb::Merger::ComputeRRFScore)
        .def("rerank", &andb::Merger::Rerank)
        .def("mark_seeds", &andb::Merger::MarkSeeds);
    
    // Standalone functions
    m.def("compute_rrf", &andb::ComputeRRF, py::arg("rank"), py::arg("k") = 60);
    m.def("compute_final_score", &andb::ComputeFinalScore);
    
    // ==========================================================================
    // Retriever (unified interface)
    // ==========================================================================
    
    py::class_<andb::Retriever>(m, "Retriever")
        .def(py::init<>())
        .def("init", &andb::Retriever::Init,
             py::arg("dense_config"),
             py::arg("sparse_index_type") = "SPARSE_INVERTED_INDEX",
             py::arg("merge_config") = andb::MergeConfig())
        .def("build", [](andb::Retriever& self,
                         py::array_t<float> dense_vectors,
                         const std::vector<andb::SparseVector>& sparse_vectors) {
            py::buffer_info buf = dense_vectors.request();
            if (buf.ndim != 2) {
                throw std::runtime_error("dense_vectors must be 2D array");
            }
            int64_t num_vectors = buf.shape[0];
            return self.Build(
                static_cast<float*>(buf.ptr),
                sparse_vectors.data(),
                num_vectors
            );
        })
        .def("retrieve", [](const andb::Retriever& self,
                            py::array_t<float> query_vector,
                            const std::string& query_text,
                            int32_t top_k,
                            bool enable_dense,
                            bool enable_sparse,
                            bool for_graph,
                            py::object filter_bitset) {
            py::buffer_info buf = query_vector.request();
            
            andb::RetrievalRequest request;
            request.query_vector = static_cast<float*>(buf.ptr);
            request.vector_dim = static_cast<int32_t>(buf.size);
            request.query_text = query_text;
            request.top_k = top_k;
            request.enable_dense = enable_dense;
            request.enable_sparse = enable_sparse;
            request.for_graph = for_graph;
            
            if (!filter_bitset.is_none()) {
                py::array_t<uint8_t> bitset = filter_bitset.cast<py::array_t<uint8_t>>();
                py::buffer_info bitset_buf = bitset.request();
                request.filter_bitset = static_cast<uint8_t*>(bitset_buf.ptr);
                request.filter_bitset_size = bitset_buf.size;
            }
            
            return self.Retrieve(request);
        },
        py::arg("query_vector"),
        py::arg("query_text") = "",
        py::arg("top_k") = 10,
        py::arg("enable_dense") = true,
        py::arg("enable_sparse") = true,
        py::arg("for_graph") = false,
        py::arg("filter_bitset") = py::none())
        .def("benchmark_retrieve", [](const andb::Retriever& self,
                                       py::array_t<float> query_vector,
                                       const std::string& query_text,
                                       bool enable_dense,
                                       bool enable_sparse,
                                       py::object filter_bitset) {
            py::buffer_info buf = query_vector.request();
            
            andb::RetrievalRequest request;
            request.query_vector = static_cast<float*>(buf.ptr);
            request.vector_dim = static_cast<int32_t>(buf.size);
            request.query_text = query_text;
            request.top_k = 10000;  // No truncation
            request.enable_dense = enable_dense;
            request.enable_sparse = enable_sparse;
            request.for_graph = false;
            
            if (!filter_bitset.is_none()) {
                py::array_t<uint8_t> bitset = filter_bitset.cast<py::array_t<uint8_t>>();
                py::buffer_info bitset_buf = bitset.request();
                request.filter_bitset = static_cast<uint8_t*>(bitset_buf.ptr);
                request.filter_bitset_size = bitset_buf.size;
            }
            
            return self.BenchmarkRetrieve(request);
        },
        py::arg("query_vector"),
        py::arg("query_text") = "",
        py::arg("enable_dense") = true,
        py::arg("enable_sparse") = true,
        py::arg("filter_bitset") = py::none())
        .def("get_dense_retriever", static_cast<andb::DenseRetriever& (andb::Retriever::*)()>(&andb::Retriever::GetDenseRetriever), py::return_value_policy::reference)
        .def("get_sparse_retriever", static_cast<andb::SparseRetriever& (andb::Retriever::*)()>(&andb::Retriever::GetSparseRetriever), py::return_value_policy::reference)
        .def("get_merger", static_cast<andb::Merger& (andb::Retriever::*)()>(&andb::Retriever::GetMerger), py::return_value_policy::reference)
        .def("is_ready", &andb::Retriever::IsReady);
}
