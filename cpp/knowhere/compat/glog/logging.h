// CogDB compat shim: replaces glog/logging.h with lightweight macros.
// Satisfies knowhere/log.h without requiring the glog system library.
#pragma once
#include <cstdio>
#include <cstdlib>

// Severity levels (mirroring glog constants)
#define INFO    0
#define WARNING 1
#define ERROR   2
#define FATAL   3

// Internal stream helper — writes to stderr and supports operator<<
#include <sstream>
#include <iostream>

namespace andb_compat_glog {
struct LogStream {
    int severity;
    const char* file;
    int line;
    std::ostringstream oss;

    LogStream(int sev, const char* f, int l) : severity(sev), file(f), line(l) {}

    template<typename T>
    LogStream& operator<<(const T& v) { oss << v; return *this; }

    ~LogStream() {
        static const char* labels[] = {"INFO","WARNING","ERROR","FATAL"};
        const char* label = (severity >= 0 && severity <= 3) ? labels[severity] : "LOG";
        std::fprintf(stderr, "[%s %s:%d] %s\n", label, file, line, oss.str().c_str());
        if (severity == FATAL) std::abort();
    }
};
struct VoidStream {
    template<typename T> VoidStream& operator<<(const T&) { return *this; }
};
} // namespace andb_compat_glog

#define LOG(severity) \
    andb_compat_glog::LogStream(severity, __FILE__, __LINE__)

// VLOG — gated by runtime verbosity (we always suppress)
#define VLOG(level) \
    if (false) andb_compat_glog::VoidStream()

// DLOG — debug-only, suppressed in release
#ifdef NDEBUG
#define DLOG(severity) if (false) andb_compat_glog::VoidStream()
#else
#define DLOG(severity) LOG(severity)
#endif

// CHECK macros
#define CHECK(cond) \
    if (!(cond)) LOG(FATAL) << "CHECK failed: " #cond " "

#define CHECK_EQ(a,b) CHECK((a)==(b))
#define CHECK_NE(a,b) CHECK((a)!=(b))
#define CHECK_LT(a,b) CHECK((a)<(b))
#define CHECK_LE(a,b) CHECK((a)<=(b))
#define CHECK_GT(a,b) CHECK((a)>(b))
#define CHECK_GE(a,b) CHECK((a)>=(b))

// glog initialisation stub
namespace google {
inline void InitGoogleLogging(const char*) {}
inline void ShutdownGoogleLogging() {}
} // namespace google
