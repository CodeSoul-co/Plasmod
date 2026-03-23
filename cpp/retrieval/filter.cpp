// Copyright 2024 CogDB Authors
// SPDX-License-Identifier: Apache-2.0
//
// Filter mechanism using bitset for Knowhere BitsetView.
// Filter is NOT a separate index - it's a bitset passed to Search().

#include "andb/filter.h"
#include <algorithm>
#include <cstring>

namespace andb {

// FilterBitset implementation

FilterBitset::FilterBitset() = default;

FilterBitset::FilterBitset(size_t num_bits) {
    Resize(num_bits);
}

FilterBitset::~FilterBitset() = default;

FilterBitset::FilterBitset(FilterBitset&&) noexcept = default;
FilterBitset& FilterBitset::operator=(FilterBitset&&) noexcept = default;

void FilterBitset::Resize(size_t num_bits) {
    num_bits_ = num_bits;
    size_t byte_size = (num_bits + 7) / 8;
    data_.resize(byte_size, 0);
}

void FilterBitset::Clear() {
    std::fill(data_.begin(), data_.end(), 0);
}

void FilterBitset::SetAll() {
    std::fill(data_.begin(), data_.end(), 0xFF);
}

void FilterBitset::Set(size_t index) {
    if (index >= num_bits_) return;
    size_t byte_idx = index / 8;
    size_t bit_idx = index % 8;
    data_[byte_idx] |= (1 << bit_idx);
}

void FilterBitset::Unset(size_t index) {
    if (index >= num_bits_) return;
    size_t byte_idx = index / 8;
    size_t bit_idx = index % 8;
    data_[byte_idx] &= ~(1 << bit_idx);
}

bool FilterBitset::Test(size_t index) const {
    if (index >= num_bits_) return true;  // Out of bounds = filtered
    size_t byte_idx = index / 8;
    size_t bit_idx = index % 8;
    return (data_[byte_idx] & (1 << bit_idx)) != 0;
}

const uint8_t* FilterBitset::Data() const {
    return data_.empty() ? nullptr : data_.data();
}

size_t FilterBitset::ByteSize() const {
    return data_.size();
}

size_t FilterBitset::NumBits() const {
    return num_bits_;
}

size_t FilterBitset::CountFiltered() const {
    size_t count = 0;
    for (size_t i = 0; i < num_bits_; ++i) {
        if (Test(i)) ++count;
    }
    return count;
}

float FilterBitset::FilterRatio() const {
    if (num_bits_ == 0) return 0.0f;
    return static_cast<float>(CountFiltered()) / static_cast<float>(num_bits_);
}

void FilterBitset::Or(const FilterBitset& other) {
    size_t min_size = std::min(data_.size(), other.data_.size());
    for (size_t i = 0; i < min_size; ++i) {
        data_[i] |= other.data_[i];
    }
}

void FilterBitset::And(const FilterBitset& other) {
    size_t min_size = std::min(data_.size(), other.data_.size());
    for (size_t i = 0; i < min_size; ++i) {
        data_[i] &= other.data_[i];
    }
}

void FilterBitset::Invert() {
    for (auto& byte : data_) {
        byte = ~byte;
    }
}

// FilterBuilder implementation

FilterBuilder::FilterBuilder() = default;
FilterBuilder::~FilterBuilder() = default;

void FilterBuilder::SetNumIds(size_t num_ids) {
    num_ids_ = num_ids;
    bitset_.Resize(num_ids);
    bitset_.Clear();  // Start with all IDs passing
}

void FilterBuilder::FilterQuarantined(const bool* quarantine_flags) {
    if (!quarantine_flags) return;
    for (size_t i = 0; i < num_ids_; ++i) {
        if (quarantine_flags[i]) {
            bitset_.Set(i);  // Filter out quarantined
        }
    }
}

void FilterBuilder::FilterExpiredTTL(const int64_t* ttl_timestamps, int64_t current_time) {
    if (!ttl_timestamps) return;
    for (size_t i = 0; i < num_ids_; ++i) {
        if (ttl_timestamps[i] > 0 && ttl_timestamps[i] < current_time) {
            bitset_.Set(i);  // Filter out expired
        }
    }
}

void FilterBuilder::FilterNotYetVisible(const int64_t* visible_times, int64_t current_time) {
    if (!visible_times) return;
    for (size_t i = 0; i < num_ids_; ++i) {
        if (visible_times[i] > current_time) {
            bitset_.Set(i);  // Filter out not yet visible
        }
    }
}

void FilterBuilder::FilterInactive(const bool* is_active_flags) {
    if (!is_active_flags) return;
    for (size_t i = 0; i < num_ids_; ++i) {
        if (!is_active_flags[i]) {
            bitset_.Set(i);  // Filter out inactive
        }
    }
}

void FilterBuilder::FilterOldVersion(const int32_t* versions, int32_t min_version) {
    if (!versions) return;
    for (size_t i = 0; i < num_ids_; ++i) {
        if (versions[i] < min_version) {
            bitset_.Set(i);  // Filter out old versions
        }
    }
}

void FilterBuilder::FilterTimeTravel(
    const int64_t* visible_times,
    const int64_t* valid_from,
    int64_t as_of_ts
) {
    for (size_t i = 0; i < num_ids_; ++i) {
        bool filter_out = false;
        
        if (visible_times && visible_times[i] > as_of_ts) {
            filter_out = true;
        }
        if (valid_from && valid_from[i] > as_of_ts) {
            filter_out = true;
        }
        
        if (filter_out) {
            bitset_.Set(i);
        }
    }
}

const FilterBitset& FilterBuilder::GetBitset() const {
    return bitset_;
}

FilterBitset FilterBuilder::TakeBitset() {
    return std::move(bitset_);
}

}  // namespace andb
