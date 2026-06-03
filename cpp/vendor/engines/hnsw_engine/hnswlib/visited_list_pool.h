#pragma once

#include <algorithm>
#include <mutex>
#include <thread>
#include <unordered_map>
#include <vector>
#include "knowhere/comp/thread_pool.h"

namespace hnswlib {

///////////////////////////////////////////////////////////
//
// Class for multi-threaded pool-management of VisitedLists
//
/////////////////////////////////////////////////////////

class VisitedListPool {
    int numelements;

 public:
    VisitedListPool(int numelements1) {
        numelements = numelements1;
    }

    // Returns a per-thread scratch vector, one per pool instance.  The
    // storage is thread_local so there is no mutex, no atomic, and no
    // cross-thread contention on the hot HNSW search path — previously
    // every call took a std::mutex, which serialised concurrent
    // searchKnn invocations and capped wall-QPS.  The per-thread map is
    // keyed by `this` so callers that query multiple indexes from the
    // same thread each get their own scratch.
    std::vector<bool>&
    getFreeVisitedList() {
        thread_local std::unordered_map<const VisitedListPool*, std::vector<bool>> tls;
        auto& res = tls[this];
        if (res.size() != (size_t)numelements) {
            res.assign(numelements, false);
        } else {
            std::fill(res.begin(), res.end(), false);
        }
        return res;
    };

    class StampList {
     public:
        void
        reset(size_t n) {
            if (marks_.size() != n) {
                marks_.assign(n, 0);
                epoch_ = 1;
                return;
            }
            ++epoch_;
            if (epoch_ == 0) {
                std::fill(marks_.begin(), marks_.end(), 0);
                epoch_ = 1;
            }
        }

        bool
        test(size_t idx) const {
            return marks_[idx] == epoch_;
        }

        void
        mark(size_t idx) {
            marks_[idx] = epoch_;
        }

        size_t
        bytes() const {
            return marks_.size() * sizeof(uint32_t);
        }

     private:
        std::vector<uint32_t> marks_;
        uint32_t epoch_ = 1;
    };

    StampList&
    getFreeVisitedStampList() {
        thread_local std::unordered_map<const VisitedListPool*, StampList> tls;
        auto& res = tls[this];
        res.reset(static_cast<size_t>(numelements));
        return res;
    }

    int64_t
    size() {
        auto threads_num = knowhere::ThreadPool::GetGlobalSearchThreadPool()->size();
        return threads_num * (sizeof(std::thread::id) + numelements * sizeof(bool)) + sizeof(*this);
    }
};
}  // namespace hnswlib
