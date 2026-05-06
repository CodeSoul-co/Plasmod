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

scripts:
python3 scripts/benchmark_standalone.py \<br/>  --indexed-dataset=data/deep/base.10M.fbin \<br/>  --query-dataset=data/deep/query.public.10K.fbin \<br/>  --groundtruth=data/deep/groundtruth.public.10K.ibin \<br/>  --indexed-count=10000000 \<br/>  --num-queries=10000 \<br/>  --topk=10

Exp table：
| **Group**  | **Layer**        | **Path**    | **Mode**                    | **Build ms** | **Batch ms** | **QPS**  | **Recall** |<br/>| ---------- | ---------------- | ----------- | --------------------------- | ------------ | ------------ | -------- | ---------- |<br/>| G1-old     | FAISS HNSW       | Native C++  | Repeated single-query batch | 677.4        | 1029.9       | 971      | 100.0%     |<br/>| G1-new     | FAISS HNSW       | Native C++  | True batch nq=1000          | 711.0        | 160.7        | 6222     | 100.0%     |<br/>| G2-old     | Knowhere HNSW    | CGO+OpenMP  | Repeated single-query batch | 745.7        | 1551.8       | 644      | 99.8%      |<br/>| G2-new     | Knowhere HNSW    | CGO+OpenMP  | OpenMP batch + plugin       | 764.2        | 253.6        | 3944     | 99.7%      |<br/>| **G2-raw** | Knowhere HNSW    | CGO+OpenMP  | Standard batch (no plugin)  | 751.9        | **244.8**    | **4084** | **99.7%**  |<br/>| G3-old     | Plasmod Bridge   | Go→CGO      | Repeated single-query batch | 765.1        | 1486.2       | 673      | 99.8%      |<br/>| G3-new     | Plasmod Bridge   | Go→CGO      | OpenMP batch + plugin       | 765.1        | 262.1        | 3815     | 99.8%      |<br/>| **G3-raw** | Plasmod Bridge   | Go→CGO      | Standard batch (no plugin)  | 766.1        | **251.0**    | **3984** | **99.7%**  |<br/>| G4-old     | Plasmod HTTP E2E | HTTP→Bridge | Repeated single-query batch | 749.1        | 1578.0       | 634      | 99.7%      |<br/>| G4-new     | Plasmod HTTP E2E | HTTP→Bridge | OpenMP batch + plugin       | 767.7        | 271.5        | 3684     | 99.8%      |<br/>| **G4-raw** | Plasmod HTTP E2E | HTTP→Bridge | Standard batch (no plugin)  | 768.0        | **257.1**    | **3890** | **99.7%**  |