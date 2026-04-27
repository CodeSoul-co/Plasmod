// CogDB compat: fmt/core.h shim — alias for format.h.
// Upstream {fmt} ships a small "core.h" with the public API surface and a
// larger "format.h" with the implementations. Our shim collapses both into
// format.h, so this file simply forwards.
#pragma once
#include "fmt/format.h"
