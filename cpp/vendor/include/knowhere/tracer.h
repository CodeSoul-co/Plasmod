// CogDB stub: opentelemetry tracing disabled.
// Replace with real opentelemetry-cpp integration when distributed tracing is needed.
#pragma once
#include <memory>
#include <string>

#define TRACE_SERVICE_KNOWHERE "knowhere"

namespace knowhere::tracer {

struct TraceConfig {
    std::string exporter;
    std::string nodeID;
    std::string roleID;
};

// No-op span / context
struct TraceSpan {
    void SetAttribute(const std::string&, const std::string&) {}
    void End() {}
};

inline void initTelemetry(const TraceConfig&) {}
inline void closeTelemetry() {}

inline std::shared_ptr<TraceSpan> StartSpan(const std::string&, void* = nullptr) {
    return std::make_shared<TraceSpan>();
}

// Macros used in HNSW source — all no-ops
#define KNOWHERE_TRACE_CTX(span, ctx)   (void)(span); (void)(ctx)
#define KNOWHERE_SET_TRACE_ATTR(sp, k, v) (void)(sp)

}  // namespace knowhere::tracer
