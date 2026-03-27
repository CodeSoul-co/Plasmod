// CogDB compat: fmt/format.h shim — no dependency on {fmt} library.
// Implements fmt::format() using std::snprintf + variadic templates.
// Supports the subset of format strings used by Knowhere (positional {} only).
#pragma once
#include <string>
#include <sstream>
#include <type_traits>

namespace fmt {

// Internal: convert value to string fragment
namespace detail {
template<typename T>
inline std::string to_str(const T& v) {
    std::ostringstream ss;
    ss << v;
    return ss.str();
}
} // namespace detail

// Base case: no arguments left, return remaining template string as-is
inline std::string format(std::string_view tmpl) {
    return std::string(tmpl);
}

// Recursive variadic: replace first "{}" with the next argument
template<typename Arg, typename... Args>
inline std::string format(std::string_view tmpl, Arg&& arg, Args&&... rest) {
    std::string result;
    result.reserve(tmpl.size() + 32);
    auto pos = tmpl.find("{}");
    if (pos == std::string_view::npos) {
        result = std::string(tmpl);
    } else {
        result += std::string(tmpl.substr(0, pos));
        result += detail::to_str(std::forward<Arg>(arg));
        auto remaining = tmpl.substr(pos + 2);
        result += fmt::format(remaining, std::forward<Args>(rest)...);
    }
    return result;
}

} // namespace fmt
