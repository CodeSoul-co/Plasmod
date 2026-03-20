// Copyright 2024 CogDB Authors
// SPDX-License-Identifier: Apache-2.0
//
// Filter mechanism using Knowhere BitsetView.
// Filter is NOT a separate index - it's a bitset passed to Search().
// All interfaces are fully exposed for extensibility.

#ifndef ANDB_FILTER_H
#define ANDB_FILTER_H

#include "andb/types.h"
#include <cstdint>
#include <memory>
#include <string>
#include <vector>

namespace andb {

// Bitset builder for constructing filter conditions
// Bit = 1 means the corresponding ID is filtered OUT (excluded from results)
class FilterBitset {
public:
    FilterBitset();
    explicit FilterBitset(size_t num_bits);
    ~FilterBitset();
    
    // Disable copy, allow move
    FilterBitset(const FilterBitset&) = delete;
    FilterBitset& operator=(const FilterBitset&) = delete;
    FilterBitset(FilterBitset&&) noexcept;
    FilterBitset& operator=(FilterBitset&&) noexcept;
    
    // Resize bitset to hold num_bits
    void Resize(size_t num_bits);
    
    // Clear all bits (all IDs pass filter)
    void Clear();
    
    // Set all bits (all IDs filtered out)
    void SetAll();
    
    // Set bit at index (filter out this ID)
    void Set(size_t index);
    
    // Clear bit at index (allow this ID)
    void Unset(size_t index);
    
    // Test if bit is set (true = filtered out)
    bool Test(size_t index) const;
    
    // Get raw data pointer for passing to Knowhere
    const uint8_t* Data() const;
    
    // Get size in bytes
    size_t ByteSize() const;
    
    // Get number of bits
    size_t NumBits() const;
    
    // Get number of filtered out bits (popcount)
    size_t CountFiltered() const;
    
    // Get filter ratio (filtered_out / total)
    float FilterRatio() const;
    
    // Combine with another bitset using OR (union of filtered IDs)
    void Or(const FilterBitset& other);
    
    // Combine with another bitset using AND (intersection of filtered IDs)
    void And(const FilterBitset& other);
    
    // Invert all bits
    void Invert();

private:
    std::vector<uint8_t> data_;
    size_t num_bits_ = 0;
};

// Filter builder for constructing complex filter conditions
// Evaluates filter expressions and builds bitset
class FilterBuilder {
public:
    FilterBuilder();
    ~FilterBuilder();
    
    // Set the total number of IDs in the index
    void SetNumIds(size_t num_ids);
    
    // Filter by quarantine flag
    // quarantine_flags: array of bool, length = num_ids
    // Filters out IDs where quarantine_flags[id] == true
    void FilterQuarantined(const bool* quarantine_flags);
    
    // Filter by TTL expiry
    // ttl_timestamps: array of int64 (unix timestamp), length = num_ids
    // current_time: current unix timestamp
    // Filters out IDs where ttl_timestamps[id] < current_time
    void FilterExpiredTTL(const int64_t* ttl_timestamps, int64_t current_time);
    
    // Filter by visible_time
    // visible_times: array of int64 (unix timestamp), length = num_ids
    // current_time: current unix timestamp
    // Filters out IDs where visible_times[id] > current_time (not yet visible)
    void FilterNotYetVisible(const int64_t* visible_times, int64_t current_time);
    
    // Filter by is_active flag
    // is_active_flags: array of bool, length = num_ids
    // Filters out IDs where is_active_flags[id] == false
    void FilterInactive(const bool* is_active_flags);
    
    // Filter by version
    // versions: array of int32, length = num_ids
    // min_version: minimum required version
    // Filters out IDs where versions[id] < min_version
    void FilterOldVersion(const int32_t* versions, int32_t min_version);
    
    // Filter by time-travel (as_of_ts)
    // visible_times: array of int64 (unix timestamp)
    // valid_from: array of int64 (unix timestamp)
    // as_of_ts: time-travel timestamp
    // Filters out IDs where visible_times[id] > as_of_ts OR valid_from[id] > as_of_ts
    void FilterTimeTravel(
        const int64_t* visible_times,
        const int64_t* valid_from,
        int64_t as_of_ts
    );
    
    // Get the built bitset
    const FilterBitset& GetBitset() const;
    
    // Move out the built bitset
    FilterBitset TakeBitset();

private:
    FilterBitset bitset_;
    size_t num_ids_ = 0;
};

}  // namespace andb

#endif  // ANDB_FILTER_H
