// Copyright 2024 CogDB Authors
// SPDX-License-Identifier: Apache-2.0
//
// Sparse retrieval — inverted posting-list index.
//
// Design
// ──────
// KnowhereSparseIndexWrapper owns an in-memory inverted index:
//
//   posting_lists_[term_id] = [(doc_id, weight), ...]  (sorted by doc_id)
//
// Build()  — inserts every (term, weight) pair from each SparseVector into the
//            corresponding posting list.
// Add()    — appends additional documents; re-sorts affected lists.
// Search() — for every query term, walks its posting list and accumulates
//            inner-product scores in a hash map; returns top-k by score.
//
// This is O(Q × L̄) at query time, where Q is the number of unique query
// terms and L̄ is the average posting-list length for those terms —
// typically orders of magnitude faster than the previous O(N × Q) brute-
// force scan.
//
// TextToSparse() produces TF-normalised sparse vectors using FNV-1a hashing
// (suitable for both indexing and querying without an external vocabulary).
//
// Serialize/Deserialize/Load/Save implement a simple little-endian binary
// format (see serialize() for layout details).

#include "andb/sparse.h"
#include <algorithm>
#include <cassert>
#include <cctype>
#include <cmath>
#include <cstdio>
#include <cstring>
#include <fstream>
#include <sstream>
#include <unordered_map>

namespace andb {

// ─── constants ────────────────────────────────────────────────────────────────

static constexpr uint32_t FNV_OFFSET_BASIS = 2166136261u;
static constexpr uint32_t FNV_PRIME        = 16777619u;
static constexpr uint32_t SPARSE_DIM       = 30000u;

// Magic bytes written at the start of every serialised index file.
static constexpr uint32_t SERIAL_MAGIC   = 0x414E4442u; // "ANDB"
static constexpr uint32_t SERIAL_VERSION = 1u;

// ─── binary helpers ───────────────────────────────────────────────────────────

static void write_u32(std::vector<uint8_t>& buf, uint32_t v) {
    buf.push_back(static_cast<uint8_t>(v));
    buf.push_back(static_cast<uint8_t>(v >> 8));
    buf.push_back(static_cast<uint8_t>(v >> 16));
    buf.push_back(static_cast<uint8_t>(v >> 24));
}

static void write_u64(std::vector<uint8_t>& buf, uint64_t v) {
    for (int i = 0; i < 8; ++i) {
        buf.push_back(static_cast<uint8_t>(v >> (i * 8)));
    }
}

static void write_f32(std::vector<uint8_t>& buf, float v) {
    uint32_t bits;
    std::memcpy(&bits, &v, 4);
    write_u32(buf, bits);
}

static bool read_u32(const uint8_t* data, size_t size, size_t& pos, uint32_t& out) {
    if (pos + 4 > size) return false;
    out = static_cast<uint32_t>(data[pos])
        | (static_cast<uint32_t>(data[pos+1]) << 8)
        | (static_cast<uint32_t>(data[pos+2]) << 16)
        | (static_cast<uint32_t>(data[pos+3]) << 24);
    pos += 4;
    return true;
}

static bool read_u64(const uint8_t* data, size_t size, size_t& pos, uint64_t& out) {
    if (pos + 8 > size) return false;
    out = 0;
    for (int i = 0; i < 8; ++i) {
        out |= (static_cast<uint64_t>(data[pos+i]) << (i * 8));
    }
    pos += 8;
    return true;
}

static bool read_f32(const uint8_t* data, size_t size, size_t& pos, float& out) {
    uint32_t bits;
    if (!read_u32(data, size, pos, bits)) return false;
    std::memcpy(&out, &bits, 4);
    return true;
}

// ─── KnowhereSparseIndexWrapper ───────────────────────────────────────────────

class KnowhereSparseIndexWrapper {
public:
    KnowhereSparseIndexWrapper() = default;
    ~KnowhereSparseIndexWrapper() = default;

    bool Init(const std::string& index_type) {
        index_type_ = index_type;
        return true;
    }

    // Build the inverted index from a batch of sparse vectors.
    bool Build(const SparseVector* vectors, int64_t num_vectors) {
        if (!vectors || num_vectors <= 0) return false;
        posting_lists_.clear();
        num_docs_ = 0;
        return append(vectors, num_vectors, /*base_id=*/0);
    }

    // Append new documents without rebuilding from scratch.
    bool Add(const SparseVector* vectors, int64_t num_vectors) {
        if (!vectors || num_vectors <= 0) return false;
        return append(vectors, num_vectors, /*base_id=*/num_docs_);
    }

    // Inverted-index search: accumulate IP scores, return top-k.
    SearchResult Search(
        const SparseVector& query,
        int32_t top_k,
        const uint8_t* filter_bitset,
        size_t filter_size
    ) const {
        SearchResult result;
        if (query.indices.empty() || top_k <= 0 || num_docs_ == 0) {
            return result;
        }

        // Accumulate inner-product scores.
        std::unordered_map<int64_t, float> scores;
        scores.reserve(std::min(static_cast<int64_t>(1024), num_docs_));

        for (size_t qi = 0; qi < query.indices.size(); ++qi) {
            uint32_t term    = query.indices[qi];
            float    q_weight = query.values[qi];
            if (q_weight == 0.0f) continue;

            auto it = posting_lists_.find(term);
            if (it == posting_lists_.end()) continue;

            for (const auto& [doc_id, doc_weight] : it->second) {
                if (is_filtered(doc_id, filter_bitset, filter_size)) continue;
                scores[doc_id] += q_weight * doc_weight;
            }
        }

        if (scores.empty()) return result;

        // Collect candidates and partial-sort for top-k.
        std::vector<std::pair<float, int64_t>> candidates;
        candidates.reserve(scores.size());
        for (auto& [doc_id, score] : scores) {
            if (score > 0.0f) candidates.emplace_back(score, doc_id);
        }

        int32_t k = std::min(static_cast<int32_t>(candidates.size()), top_k);
        std::partial_sort(
            candidates.begin(),
            candidates.begin() + k,
            candidates.end(),
            [](const auto& a, const auto& b) { return a.first > b.first; }
        );

        result.ids.reserve(k);
        result.distances.reserve(k);
        for (int32_t i = 0; i < k; ++i) {
            result.ids.push_back(candidates[i].second);
            result.distances.push_back(candidates[i].first);
        }
        result.count = k;
        return result;
    }

    // ── Serialisation ─────────────────────────────────────────────────────────
    //
    // Binary layout (little-endian):
    //   [0..3]  magic   ANDB (0x41 0x4E 0x44 0x42)
    //   [4..7]  version uint32 = 1
    //   [8..15] num_docs uint64
    //   [16..19] num_terms uint32
    //   for each term:
    //     term_id   uint32
    //     num_postings uint32
    //     for each posting:
    //       doc_id uint64
    //       weight float32

    bool Serialize(std::vector<uint8_t>& output) const {
        output.clear();
        write_u32(output, SERIAL_MAGIC);
        write_u32(output, SERIAL_VERSION);
        write_u64(output, static_cast<uint64_t>(num_docs_));
        write_u32(output, static_cast<uint32_t>(posting_lists_.size()));

        for (const auto& [term_id, postings] : posting_lists_) {
            write_u32(output, term_id);
            write_u32(output, static_cast<uint32_t>(postings.size()));
            for (const auto& [doc_id, weight] : postings) {
                write_u64(output, static_cast<uint64_t>(doc_id));
                write_f32(output, weight);
            }
        }
        return true;
    }

    bool Deserialize(const std::vector<uint8_t>& input) {
        const uint8_t* data = input.data();
        size_t size = input.size();
        size_t pos = 0;

        uint32_t magic, version, num_terms;
        uint64_t num_docs;

        if (!read_u32(data, size, pos, magic)   || magic != SERIAL_MAGIC) return false;
        if (!read_u32(data, size, pos, version) || version != SERIAL_VERSION) return false;
        if (!read_u64(data, size, pos, num_docs)) return false;
        if (!read_u32(data, size, pos, num_terms)) return false;

        posting_lists_.clear();
        posting_lists_.reserve(num_terms);

        for (uint32_t t = 0; t < num_terms; ++t) {
            uint32_t term_id, num_postings;
            if (!read_u32(data, size, pos, term_id))     return false;
            if (!read_u32(data, size, pos, num_postings)) return false;

            auto& list = posting_lists_[term_id];
            list.reserve(num_postings);
            for (uint32_t p = 0; p < num_postings; ++p) {
                uint64_t doc_id_u64;
                float weight;
                if (!read_u64(data, size, pos, doc_id_u64)) return false;
                if (!read_f32(data, size, pos, weight))      return false;
                list.emplace_back(static_cast<int64_t>(doc_id_u64), weight);
            }
        }

        num_docs_ = static_cast<int64_t>(num_docs);
        return true;
    }

    int64_t     Count() const { return num_docs_; }
    std::string Type()  const { return index_type_; }

private:
    std::string index_type_{"SPARSE_INVERTED_INDEX"};
    int64_t     num_docs_{0};

    // posting_lists_[term_id] = [(doc_id, weight), ...]
    std::unordered_map<uint32_t, std::vector<std::pair<int64_t, float>>> posting_lists_;

    // Append `num_vectors` documents starting at base_id.
    bool append(const SparseVector* vectors, int64_t num_vectors, int64_t base_id) {
        for (int64_t i = 0; i < num_vectors; ++i) {
            int64_t doc_id = base_id + i;
            const SparseVector& sv = vectors[i];
            for (size_t j = 0; j < sv.indices.size(); ++j) {
                posting_lists_[sv.indices[j]].emplace_back(doc_id, sv.values[j]);
            }
        }
        num_docs_ = base_id + num_vectors;
        return true;
    }

    static bool is_filtered(int64_t doc_id,
                            const uint8_t* bitset,
                            size_t bitset_size) {
        if (!bitset || bitset_size == 0) return false;
        size_t byte_idx = static_cast<size_t>(doc_id) / 8;
        size_t bit_idx  = static_cast<size_t>(doc_id) % 8;
        return byte_idx < bitset_size && ((bitset[byte_idx] >> bit_idx) & 1u);
    }
};

// ─── SparseRetriever — public interface ───────────────────────────────────────

SparseRetriever::SparseRetriever()
    : impl_(std::make_unique<KnowhereSparseIndexWrapper>()) {}

SparseRetriever::~SparseRetriever() = default;
SparseRetriever::SparseRetriever(SparseRetriever&&) noexcept = default;
SparseRetriever& SparseRetriever::operator=(SparseRetriever&&) noexcept = default;

bool SparseRetriever::Init(const std::string& index_type) {
    index_type_ = index_type;
    ready_ = impl_->Init(index_type);
    return ready_;
}

bool SparseRetriever::Build(const SparseVector* vectors, int64_t num_vectors) {
    if (!impl_->Build(vectors, num_vectors)) return false;
    ready_ = true;
    return true;
}

bool SparseRetriever::Add(const SparseVector* vectors, int64_t num_vectors) {
    return impl_->Add(vectors, num_vectors);
}

SearchResult SparseRetriever::Search(
    const SparseVector& query,
    int32_t top_k,
    const uint8_t* filter_bitset,
    size_t filter_size
) const {
    if (!ready_) return SearchResult{};
    return impl_->Search(query, top_k, filter_bitset, filter_size);
}

// ─── TextToSparse ─────────────────────────────────────────────────────────────
//
// Converts raw text to a TF-normalised sparse vector using FNV-1a hashing.
//
// Pipeline:
//   1. Lowercase
//   2. Tokenise on whitespace
//   3. Hash each token: term_id = FnvHash(token) % SPARSE_DIM
//   4. Count token frequencies (term frequency, TF)
//   5. Normalise by total token count → TF weights in (0, 1]
//   6. Sort result by index for O(|q| + |d|) sparse-IP during search
//
// Using the same function for both documents and queries produces an
// unweighted cosine-like similarity (TF × TF dot product).  For BM25 IDF
// weighting, collect document frequencies during Build() and call an
// index-aware scorer instead.

// static
uint32_t SparseRetriever::FnvHash(const std::string& token) {
    uint32_t hash = FNV_OFFSET_BASIS;
    for (unsigned char c : token) {
        hash ^= c;
        hash *= FNV_PRIME;
    }
    return hash;
}

// static
SparseVector SparseRetriever::TextToSparse(const std::string& text) {
    SparseVector result;
    if (text.empty()) return result;

    // Tokenise + lowercase.
    std::unordered_map<uint32_t, float> term_counts;
    int total_tokens = 0;

    std::istringstream iss(text);
    std::string token;
    while (iss >> token) {
        for (char& c : token) {
            c = static_cast<char>(std::tolower(static_cast<unsigned char>(c)));
        }
        uint32_t idx = FnvHash(token) % SPARSE_DIM;
        term_counts[idx] += 1.0f;
        ++total_tokens;
    }

    if (total_tokens == 0) return result;

    // Normalise.
    float norm = 1.0f / static_cast<float>(total_tokens);
    result.indices.reserve(term_counts.size());
    result.values.reserve(term_counts.size());
    for (auto& [idx, count] : term_counts) {
        result.indices.push_back(idx);
        result.values.push_back(count * norm);
    }

    // Sort by index for efficient two-pointer sparse-IP in Search().
    std::vector<std::pair<uint32_t, float>> pairs;
    pairs.reserve(result.indices.size());
    for (size_t i = 0; i < result.indices.size(); ++i) {
        pairs.emplace_back(result.indices[i], result.values[i]);
    }
    std::sort(pairs.begin(), pairs.end());

    result.indices.clear();
    result.values.clear();
    for (auto& [idx, val] : pairs) {
        result.indices.push_back(idx);
        result.values.push_back(val);
    }

    return result;
}

// ─── Serialisation wrappers ───────────────────────────────────────────────────

bool SparseRetriever::Serialize(std::vector<uint8_t>& output) const {
    return impl_->Serialize(output);
}

bool SparseRetriever::Deserialize(const std::vector<uint8_t>& input) {
    if (!impl_->Deserialize(input)) return false;
    ready_ = (impl_->Count() > 0);
    return true;
}

bool SparseRetriever::Load(const std::string& path) {
    std::ifstream ifs(path, std::ios::binary | std::ios::ate);
    if (!ifs.is_open()) return false;
    std::streamsize sz = ifs.tellg();
    if (sz <= 0) return false;
    ifs.seekg(0, std::ios::beg);

    std::vector<uint8_t> buf(static_cast<size_t>(sz));
    if (!ifs.read(reinterpret_cast<char*>(buf.data()), sz)) return false;
    return Deserialize(buf);
}

bool SparseRetriever::Save(const std::string& path) const {
    std::vector<uint8_t> buf;
    if (!Serialize(buf)) return false;

    std::ofstream ofs(path, std::ios::binary | std::ios::trunc);
    if (!ofs.is_open()) return false;
    ofs.write(reinterpret_cast<const char*>(buf.data()),
              static_cast<std::streamsize>(buf.size()));
    return ofs.good();
}

// ─── Misc ─────────────────────────────────────────────────────────────────────

int64_t     SparseRetriever::Count()   const { return impl_->Count(); }
std::string SparseRetriever::Type()    const { return impl_->Type(); }
bool        SparseRetriever::IsReady() const { return ready_; }

}  // namespace andb
