# Layer 1 Experiment Runbook

This folder contains the current Layer 1 fair benchmark environment.

## Scripts

Only the current Layer 1 experiment scripts should be kept in `scripts/`:

- `scripts/build_for_experiments.sh`
- `scripts/layer1_fair_benchmark.py`

## Build Before Experiments

Always rebuild before running an experiment if Go or C++ code changed:

```bash
cd /Users/erwin/Downloads/codespace/Plasmod
bash plasmod_test_env/scripts/build_for_experiments.sh
```

This script rebuilds:

1. `cpp/build/libplasmod_retrieval.dylib`
2. `cpp/build/vendor/libknowhere.dylib`
3. `bin/plasmod`
4. `plasmod_test_env/bin/plasmod`

The experiment server uses:

```text
/Users/erwin/Downloads/codespace/Plasmod/bin/plasmod
```

So if the source code changed but the binary was not rebuilt, the experiment will run old code.

## Why Release Build Matters

`CMAKE_BUILD_TYPE=Release` tells CMake to compile C++ code in optimized release mode.

Release mode usually enables compiler flags like:

```text
-O3 -DNDEBUG
```

Meaning:

- `-O3`: optimize the generated machine code for speed.
- `-DNDEBUG`: disable debug assertions.

For retrieval benchmarks, this is required. Running Knowhere/HNSW with an empty or debug-like CMake build type can make the C++ index much slower and produce misleading benchmark results.

The build script now forces Release by default:

```bash
BUILD_TYPE="${BUILD_TYPE:-Release}"
cmake -S cpp -B cpp/build -DCMAKE_BUILD_TYPE="${BUILD_TYPE}" ...
```

To intentionally build another type:

```bash
BUILD_TYPE=RelWithDebInfo bash plasmod_test_env/scripts/build_for_experiments.sh
```

Do not use Debug results for performance claims.

## When Rebuild Is Needed

Rebuild with `build_for_experiments.sh` when changing:

- `cpp/` C++ retrieval, Knowhere, HNSW, CMake, or native library code.
- `src/` Go server code.
- CGO bridge code under `src/internal/dataplane/retrievalplane`.

Rebuild is usually not needed when changing:

- `plasmod_test_env/scripts/layer1_fair_benchmark.py`
- result formatting
- benchmark output file names
- dataset files
- command-line benchmark parameters

## Running Layer 1 Fair Benchmark

After building, run:

```bash
cd /Users/erwin/Downloads/codespace/Plasmod/plasmod_test_env
python3 scripts/layer1_fair_benchmark.py --limit 10000 --num-queries 1000 --topk 10
```

This benchmark is `kernel_direct` only:

- FAISS HNSW direct
- Plasmod Knowhere direct
- same dataset
- same normalization
- same top-k
- same HNSW parameters
- no HTTP
- no embedding
- no storage
- no object graph
- no policy/version/provenance

Do not compare Plasmod Full HTTP results against FAISS in-process kernel results. Full-system benchmarks require a baseline with the same HTTP, embedding, storage, filtering, and persistence boundaries.
