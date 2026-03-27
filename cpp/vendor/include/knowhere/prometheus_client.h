// CogDB stub: prometheus metrics disabled.
// Replace with real prometheus-cpp integration when monitoring is needed.
#pragma once
#include <string>
#include "knowhere/log.h"

namespace knowhere {

// No-op counter/gauge/histogram stubs
struct MetricFamily { void Add(std::string, std::string) {} };
struct Counter      { void Increment(double = 1.0) {}      };
struct Gauge        { void Set(double) {} void Increment(double=1.0){} void Decrement(double=1.0){} };
struct Histogram    { void Observe(double) {}               };

class PrometheusClient {
public:
    PrometheusClient()  = default;
    ~PrometheusClient() = default;

    // All metric registration is a no-op
    template<typename... Args>
    PrometheusClient& RegisterCounter(Args&&...)  { return *this; }
    template<typename... Args>
    PrometheusClient& RegisterGauge(Args&&...)    { return *this; }
    template<typename... Args>
    PrometheusClient& RegisterHistogram(Args&&...) { return *this; }

    std::string Serialize() const { return ""; }
};

inline PrometheusClient& prometheusClient() {
    static PrometheusClient client;
    return client;
}

}  // namespace knowhere
