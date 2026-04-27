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

    int64_t
    size() {
        auto threads_num = knowhere::ThreadPool::GetGlobalSearchThreadPool()->size();
        return threads_num * (sizeof(std::thread::id) + numelements * sizeof(bool)) + sizeof(*this);
    }
};
}  // namespace hnswlib
