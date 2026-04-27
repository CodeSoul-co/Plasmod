# Server Migration (Member A)

This document records Member A deliverables for Linux server migration tasks:

- Task 4: S3/MinIO full E2E integration test plan and execution entrypoint
- Task 5: Full Go unit test suite in Docker + expected failures and causes
- Task 6: Environment variable matrix for migration scenarios

## Task 4 — S3/MinIO Full E2E Integration

### Preconditions

- Docker daemon is reachable by current user.
- Required images are available from your configured registry (internet or intranet mirror).
- `plasmod` service can boot (`GET /healthz` returns 200).

### Run

```bash
docker compose up -d minio minio-init plasmod
PLASMOD_BASE_URL=http://127.0.0.1:8080 python3 scripts/e2e/member_a_capture.py --out-dir ./out/member_a_fullstack_verify
```

Or use the bundled one-command script:

```bash
bash scripts/e2e/run_acceptance_scenario_a.sh
```

### What to verify

1. Ingest/query cycle succeeds against MinIO-backed cold tier.
2. `include_cold=true` query path returns structured response.
3. Output JSON files include:
   - `response.proof_trace`
   - `response.evidence_cache`
4. MinIO hard check passes (write + stat):

```bash
docker compose run --rm --entrypoint /bin/sh minio-init -lc \
  'mc alias set local http://minio:9000 minioadmin minioadmin >/dev/null && \
   printf "{\"ok\":true}\n" | mc pipe local/plasmod-integration/plasmod/task4_probe.json >/dev/null && \
   mc stat local/plasmod-integration/plasmod/task4_probe.json >/dev/null'
```

Notes:

- Fixture path should be explicitly passed with `--fixtures` for reproducibility (the capture script supports a fallback search order).
- The capture script emits one JSON result per scenario to the output directory.

## Task 5 — Go Unit Tests in Docker

### Command

```bash
docker build --platform=linux/amd64 --target builder -t cogdb:test-builder .
docker run --rm -v "$PWD:/src" -w /src --platform=linux/amd64 \
  cogdb:test-builder /bin/sh -lc \
  '/usr/local/go/bin/go test $(/usr/local/go/bin/go list ./src/internal/... | grep -v "^plasmod/src/internal/app$" | grep -v "^plasmod/src/internal/dataplane/embedding$") -count=1 -timeout 120s'
```

### Expected failures and root causes

The following failures are expected in environments that do not provide required native/runtime dependencies:

1. **Embedding providers requiring external runtimes**  
   Root cause: ONNX/GGUF/TensorRT providers depend on native runtime libraries, model files, or CUDA stack not always present in base server image.
   - ONNX: missing `libonnxruntime.so` and/or model file.
   - GGUF: missing `go-llama.cpp` CUDA/Metal runtime requirements or model file.
   - TensorRT: requires Linux + CUDA + TensorRT runtime and engine/model artifacts.

2. **CGO/retrieval optional native library path issues (if retrieval tag is enabled)**  
   Root cause: `libplasmod_retrieval.so` not built or not visible via linker/runtime path.

### Triage guidance

- If failure is in provider availability (`ErrProviderUnavailable`), classify as environment/runtime dependency.
- If failure is compile/link related (`cannot find -l...`, missing `.so`), classify as native library linkage issue.
- Keep failures that are unrelated to environment dependencies as real regressions.

## Task 6 — Environment Variable Matrix

Use this matrix when switching deployment scenarios.

| Scenario | Required environment variables |
|---|---|
| In-memory only (local smoke) | `PLASMOD_STORAGE=inmemory`, `PLASMOD_EMBEDDER=tfidf` |
| Disk only | `PLASMOD_STORAGE=disk`, `PLASMOD_DATA_DIR=/data`, `PLASMOD_EMBEDDER=tfidf` |
| ONNX CPU | `PLASMOD_STORAGE=disk`, `PLASMOD_DATA_DIR=/data`, `PLASMOD_EMBEDDER=onnx`, `PLASMOD_EMBEDDER_DEVICE=cpu` |
| ONNX CUDA | `PLASMOD_STORAGE=disk`, `PLASMOD_DATA_DIR=/data`, `PLASMOD_EMBEDDER=onnx`, `PLASMOD_EMBEDDER_DEVICE=cuda` |
| GGUF CUDA | `PLASMOD_STORAGE=disk`, `PLASMOD_DATA_DIR=/data`, `PLASMOD_EMBEDDER=gguf`, `PLASMOD_EMBEDDER_DEVICE=cuda` |
| TensorRT CUDA | `PLASMOD_STORAGE=disk`, `PLASMOD_DATA_DIR=/data`, `PLASMOD_EMBEDDER=tensorrt`, `PLASMOD_EMBEDDER_DEVICE=cuda` |
| Metal (macOS) | `PLASMOD_STORAGE=disk`, `PLASMOD_DATA_DIR=/data`, `PLASMOD_EMBEDDER=gguf` or `onnx`, `PLASMOD_EMBEDDER_DEVICE=metal` |
| S3/MinIO cold tier enabled | `S3_ENDPOINT`, `S3_ACCESS_KEY`, `S3_SECRET_KEY`, `S3_BUCKET`, `S3_SECURE`, `S3_REGION`, `S3_PREFIX` |
| S3/MinIO cold query protection (recommended) | `S3_COLD_MAX_PAGES` (default `20`), `S3_COLD_MAX_CANDIDATES` (default `1000`) |

Canonical values:

- `PLASMOD_STORAGE=disk|inmemory`
- `PLASMOD_DATA_DIR=/data`
- `ANDB_EMBEDDER=tfidf|openai|zhipuai|onnx|gguf|tensorrt`
- `ANDB_EMBEDDER_DEVICE=cpu|cuda|metal`
- `S3_ENDPOINT`, `S3_ACCESS_KEY`, `S3_SECRET_KEY`, `S3_BUCKET`, `S3_SECURE`, `S3_REGION`, `S3_PREFIX`
- `S3_COLD_MAX_PAGES` (hard cap for ListObjects pages in cold search)
- `S3_COLD_MAX_CANDIDATES` (hard cap for cold candidates scanned)

Compatibility note:

- Legacy `S3_COLDSEARCH_MAX_KEYS` is still supported as a fallback if `S3_COLD_MAX_CANDIDATES` is not set.

## Current execution status

In this workspace, migration artifacts and scripts are prepared, but runtime validation depends on:

- Docker permission to access `/var/run/docker.sock`
- Reachable container/image sources (public or intranet mirror)

