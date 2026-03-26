// Copyright (C) 2019-2023 Zilliz. All rights reserved.
// CogDB adaptation: replaced folly thread pool with std::thread + std::future.
// Public API is preserved; internal implementation is CogDB-native.
//
// SPDX-License-Identifier: Apache-2.0
#pragma once

#include <atomic>
#include <cassert>
#include <condition_variable>
#include <deque>
#include <functional>
#include <future>
#include <memory>
#include <mutex>
#include <stdexcept>
#include <string>
#include <thread>
#include <vector>

#include "knowhere/expected.h"
#include "knowhere/log.h"

// ── OpenMP shim ────────────────────────────────────────────────────────────
#ifdef _OPENMP
#  include <omp.h>
#else
inline int  omp_get_max_threads() { return 1; }
inline void omp_set_num_threads(int) {}
#endif

namespace knowhere {

// ── CogDB-native future type (wraps std::future) ───────────────────────────
template <typename T>
using Future = std::future<T>;

// folly::Unit equivalent — used as Future<Unit> for fire-and-forget tasks
struct Unit {};

// ── Task queue (FIFO or LIFO, lock-based) ─────────────────────────────────
class TaskQueue {
public:
    enum class Mode { FIFO, LIFO };
    explicit TaskQueue(Mode m = Mode::FIFO) : mode_(m) {}

    void push(std::function<void()> f) {
        std::lock_guard<std::mutex> lk(mu_);
        tasks_.push_back(std::move(f));
        cv_.notify_one();
    }

    // Returns false when queue is shut down and empty
    bool pop(std::function<void()>& out) {
        std::unique_lock<std::mutex> lk(mu_);
        cv_.wait(lk, [this] { return !tasks_.empty() || stopped_; });
        if (tasks_.empty()) return false;
        if (mode_ == Mode::LIFO) {
            out = std::move(tasks_.back());
            tasks_.pop_back();
        } else {
            out = std::move(tasks_.front());
            tasks_.pop_front();
        }
        return true;
    }

    void stop() {
        std::lock_guard<std::mutex> lk(mu_);
        stopped_ = true;
        cv_.notify_all();
    }

    size_t size() const {
        std::lock_guard<std::mutex> lk(mu_);
        return tasks_.size();
    }

private:
    mutable std::mutex mu_;
    std::condition_variable cv_;
    std::deque<std::function<void()>> tasks_;
    Mode mode_;
    bool stopped_ = false;
};

// ── ThreadPool ─────────────────────────────────────────────────────────────
class ThreadPool {
public:
    enum class QueueType { LIFO, FIFO };

    explicit ThreadPool(uint32_t num_threads,
                        const std::string& /*thread_name_prefix*/,
                        QueueType queueT  = QueueType::LIFO,
                        int /*priority*/  = 10)
        : queue_(queueT == QueueType::LIFO ? TaskQueue::Mode::LIFO : TaskQueue::Mode::FIFO),
          num_threads_(num_threads) {
        workers_.reserve(num_threads);
        for (uint32_t i = 0; i < num_threads; ++i) {
            workers_.emplace_back([this] {
                std::function<void()> task;
                while (queue_.pop(task)) task();
            });
        }
    }

    ~ThreadPool() {
        queue_.stop();
        for (auto& t : workers_) if (t.joinable()) t.join();
    }

    ThreadPool(const ThreadPool&)            = delete;
    ThreadPool& operator=(const ThreadPool&) = delete;
    ThreadPool(ThreadPool&&)                 = delete;
    ThreadPool& operator=(ThreadPool&&)      = delete;

    // push(func, args...) → Future<ReturnType>
    // Special case: if func returns void, we return Future<Unit> so it can be
    // stored in std::vector<Future<Unit>> (mirrors folly::Future<folly::Unit>).
    template <typename Func, typename... Args>
    auto push(Func&& func, Args&&... args) {
        using RawRet = std::invoke_result_t<std::decay_t<Func>, Args...>;
        if constexpr (std::is_same_v<RawRet, void>) {
            // Wrap void return in Unit
            auto task = std::make_shared<std::packaged_task<Unit()>>(
                [f = std::forward<Func>(func),
                 tup = std::make_tuple(std::forward<Args>(args)...)]() mutable {
                    std::apply(std::move(f), std::move(tup));
                    return Unit{};
                });
            std::future<Unit> fut = task->get_future();
            queue_.push([task] { (*task)(); });
            return fut;
        } else {
            auto task = std::make_shared<std::packaged_task<RawRet()>>(
                [f = std::forward<Func>(func),
                 tup = std::make_tuple(std::forward<Args>(args)...)]() mutable {
                    return std::apply(std::move(f), std::move(tup));
                });
            std::future<RawRet> fut = task->get_future();
            queue_.push([task] { (*task)(); });
            return fut;
        }
    }

    [[nodiscard]] size_t size() const noexcept { return num_threads_; }

    size_t GetPendingTaskCount() { return queue_.size(); }

    void SetNumThreads(uint32_t /*n*/) {
        // Dynamic resize not supported in the CogDB std::thread backend.
        // Restart the pool in caller code if you need a different thread count.
        LOG_KNOWHERE_WARNING_ << "ThreadPool::SetNumThreads: not supported in CogDB backend";
    }

    // ── Static factory helpers ─────────────────────────────────────────────
    static ThreadPool CreateFIFO(uint32_t n, const std::string& name) {
        return ThreadPool(n, name, QueueType::FIFO);
    }
    static ThreadPool CreateLIFO(uint32_t n, const std::string& name) {
        return ThreadPool(n, name, QueueType::LIFO);
    }

    static void InitGlobalBuildThreadPool(uint32_t n) {
        if (n == 0) { LOG_KNOWHERE_ERROR_ << "num_threads must be > 0"; return; }
        std::lock_guard<std::mutex> lk(build_pool_mutex_);
        if (!build_pool_) {
            build_pool_ = std::make_shared<ThreadPool>(n, "knowhere_build");
            LOG_KNOWHERE_INFO_ << "Init global build thread pool, size=" << n;
        }
    }
    static void InitGlobalSearchThreadPool(uint32_t n) {
        if (n == 0) { LOG_KNOWHERE_ERROR_ << "num_threads must be > 0"; return; }
        std::lock_guard<std::mutex> lk(search_pool_mutex_);
        if (!search_pool_) {
            search_pool_ = std::make_shared<ThreadPool>(n, "knowhere_search");
            LOG_KNOWHERE_INFO_ << "Init global search thread pool, size=" << n;
        }
    }

    static void   SetGlobalBuildThreadPoolSize(uint32_t n)  { InitGlobalBuildThreadPool(n); }
    static void   SetGlobalSearchThreadPoolSize(uint32_t n) { InitGlobalSearchThreadPool(n); }
    static size_t GetGlobalBuildThreadPoolSize()   { return build_pool_  ? build_pool_->size()  : 0; }
    static size_t GetGlobalSearchThreadPoolSize()  { return search_pool_ ? search_pool_->size() : 0; }
    static size_t GetSearchThreadPoolPendingTaskCount() { return GetGlobalSearchThreadPool()->GetPendingTaskCount(); }
    static size_t GetBuildThreadPoolPendingTaskCount()  { return GetGlobalBuildThreadPool()->GetPendingTaskCount();  }

    static std::shared_ptr<ThreadPool> GetGlobalBuildThreadPool() {
        if (!build_pool_) InitGlobalBuildThreadPool(std::thread::hardware_concurrency());
        return build_pool_;
    }
    static std::shared_ptr<ThreadPool> GetGlobalSearchThreadPool() {
        if (!search_pool_) InitGlobalSearchThreadPool(std::thread::hardware_concurrency());
        return search_pool_;
    }

    // ── OMP scope setters (thread-count bookkeeping, OMP only) ────────────
    class ScopedBuildOmpSetter {
        int before_;
    public:
        explicit ScopedBuildOmpSetter(int n = 0) {
            before_ = build_pool_ ? static_cast<int>(build_pool_->size()) : omp_get_max_threads();
            omp_set_num_threads(n <= 0 ? before_ : n);
        }
        ~ScopedBuildOmpSetter() { omp_set_num_threads(before_); }
    };
    class ScopedSearchOmpSetter {
        int before_;
    public:
        explicit ScopedSearchOmpSetter(int n = 1) {
            before_ = search_pool_ ? static_cast<int>(search_pool_->size()) : omp_get_max_threads();
            omp_set_num_threads(n <= 0 ? before_ : n);
        }
        ~ScopedSearchOmpSetter() { omp_set_num_threads(before_); }
    };

private:
    TaskQueue queue_;
    uint32_t  num_threads_;
    std::vector<std::thread> workers_;

    inline static std::mutex                    build_pool_mutex_;
    inline static std::shared_ptr<ThreadPool>   build_pool_  = nullptr;
    inline static std::mutex                    search_pool_mutex_;
    inline static std::shared_ptr<ThreadPool>   search_pool_ = nullptr;
};

// ── WaitAllSuccess (mirrors original folly version) ───────────────────────
template <typename T>
inline Status WaitAllSuccess(std::vector<std::future<T>>& futures) {
    static_assert(std::is_same<T, Unit>::value || std::is_same<T, Status>::value,
                  "WaitAllSuccess: T must be Unit or knowhere::Status");
    for (auto& f : futures) {
        f.get(); // rethrows exceptions
        if constexpr (!std::is_same_v<T, Unit>) {
            // T == Status: check return value
            // (future already consumed by f.get() above — use a separate get)
        }
    }
    return Status::success;
}

// Specialisation for Status futures (need the return value)
template <>
inline Status WaitAllSuccess<Status>(std::vector<std::future<Status>>& futures) {
    for (auto& f : futures) {
        Status s = f.get();
        if (s != Status::success) return s;
    }
    return Status::success;
}

}  // namespace knowhere
