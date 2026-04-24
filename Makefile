.PHONY: dev build test integration-test integration-test-s3 cpp sdk-python fmt docker-up docker-down member-a-capture member-a-verify member-a-gpu-check member-a-task4-strict member-a-all prod-safety-check setup bench-layer1 bench-retrieval

# Default MinIO settings for local S3/MinIO integration tests.
# Override these when invoking make if your MinIO differs.
S3_ENDPOINT ?= 127.0.0.1:9000
S3_ACCESS_KEY ?= minioadmin
S3_SECRET_KEY ?= minioadmin
S3_BUCKET ?= andb-integration
S3_SECURE ?= false
S3_REGION ?= us-east-1
S3_PREFIX ?= andb/integration_tests

# RETRIEVAL_TAG enables the CGO Knowhere/HNSW retriever.
# It is only safe to set when cpp/build/libplasmod_retrieval.so/dylib exists.
# Use `make cpp` to build the C++ library and set this automatically.
RETRIEVAL_TAG :=
CPP_LIB := cpp/build/libplasmod_retrieval.dylib
CPP_LIB_SO := cpp/build/libplasmod_retrieval.so
ifeq ($(shell [ -f $(CPP_LIB) ] && echo yes),yes)
  RETRIEVAL_TAG := -tags retrieval
  CGO_LDFLAGS := -L$(shell pwd)/cpp/build -lplasmod_retrieval -Wl,-rpath,$(shell pwd)/cpp/build
else ifeq ($(shell [ -f $(CPP_LIB_SO) ] && echo yes),yes)
  RETRIEVAL_TAG := -tags retrieval
  CGO_LDFLAGS := -L$(shell pwd)/cpp/build -lplasmod_retrieval -Wl,-rpath,$(shell pwd)/cpp/build
endif

setup:
	go mod download
	bash scripts/setup_env.sh
	cd sdk/nodejs && npm install

dev:
	CGO_LDFLAGS="$(CGO_LDFLAGS)" RETRIEVAL_TAG="$(RETRIEVAL_TAG)" bash scripts/dev_up.sh

build:
	bash -c 'set -a; [ -f .env ] && source .env; set +a; CGO_LDFLAGS="$(CGO_LDFLAGS)" go build $(RETRIEVAL_TAG) -o bin/plasmod ./src/cmd/server'

cpp:
	cmake -S cpp -B cpp/build && cmake --build cpp/build --parallel $(shell nproc)

cpp-gpu:
	cmake -S cpp -B cpp/build -DANDB_WITH_GPU=ON && cmake --build cpp/build --parallel $(shell nproc)

tensorrt:
	cmake -S cpp -B cpp/build_trt -DANDB_WITH_TENSORRT=ON -DANDB_WITH_GPU=OFF
	cmake --build cpp/build_trt --target plasmod_tensorrt --parallel $(shell nproc)
	CGO_CFLAGS="-I/usr/local/cuda-12.9/include -I/usr/include/x86_64-linux-gnu" \
	CGO_LDFLAGS="-L$(shell pwd)/cpp/build_trt -lplasmod_tensorrt -lcudart -lnvinfer -Wl,-rpath,$(shell pwd)/cpp/build_trt" \
	go build -tags cuda,tensorrt,linux ./src/...

tensorrt-dev:
	bash -c 'set -a; [ -f .env ] && source .env; set +a; \
	CGO_CFLAGS="-I/usr/local/cuda-12.9/include -I/usr/include/x86_64-linux-gnu" \
	CGO_LDFLAGS="-L$(shell pwd)/cpp/build_trt -lplasmod_tensorrt -lcudart -lnvinfer -Wl,-rpath,$(shell pwd)/cpp/build_trt" \
	go run -tags cuda,tensorrt,linux ./src/cmd/server'

gguf:
	$(MAKE) -C libs/go-llama.cpp -j$(shell nproc)
	go build -tags gguf ./src/...

gguf-dev:
	bash -c 'set -a; [ -f .env ] && source .env; set +a; CGO_LDFLAGS="$(CGO_LDFLAGS)" go run -tags gguf ./src/cmd/server'

# cpp-with-knowhere builds the full C++ stack including Knowhere HNSW.
# Requires: libomp, folly, prometheus-cpp, opentelemetry-cpp installed via Homebrew.
# Set CMAKE_PREFIX_PATH=/opt/homebrew when using Homebrew-installed deps.
# Run `make cpp` (without knowhere) first to build the stub, then upgrade.
cpp-with-knowhere:
	cmake -S cpp -B cpp/build \
	  -DANDB_WITH_KNOWHERE=ON \
	  -DOpenMP_C_FLAGS="-Xclang -fopenmp -I/opt/homebrew/Cellar/libomp/22.1.1/include" \
	  -DOpenMP_omp_LIBRARY="/opt/homebrew/Cellar/libomp/22.1.1/lib/libomp.dylib" \
	  -DCMAKE_PREFIX_PATH="/opt/homebrew" && cmake --build cpp/build

sdk-python:
	pip install -e ./sdk/python

test:
	go test ./src/...
	pytest -q

# Run full integration test suite against a running server (default: http://127.0.0.1:8080).
# Go tests cover all HTTP API routes; Python tests validate the Python SDK (AndbClient).
#
# Optional S3/MinIO dataflow test:
#   ANDB_RUN_S3_TESTS=true \
#   S3_ENDPOINT=127.0.0.1:9000 S3_ACCESS_KEY=minioadmin \
#   S3_SECRET_KEY=minioadmin S3_BUCKET=andb-integration \
#   make integration-test
integration-test:
	go test ./integration_tests/... -v -timeout 120s
	cd integration_tests/python && python run_all.py

integration-test-s3:
	ANDB_RUN_S3_TESTS=true \
	S3_ENDPOINT=$(S3_ENDPOINT) S3_ACCESS_KEY=$(S3_ACCESS_KEY) \
	S3_SECRET_KEY=$(S3_SECRET_KEY) S3_BUCKET=$(S3_BUCKET) \
	S3_SECURE=$(S3_SECURE) S3_REGION=$(S3_REGION) S3_PREFIX=$(S3_PREFIX) \
	$(MAKE) integration-test

fmt:
	gofmt -w $(shell find src -name '*.go')

# Docker Compose: MinIO + ANDB with disk storage and S3 cold tier (see README Integration Tests).
docker-up:
	docker compose up -d

docker-down:
	docker compose down

# Requires a running server (e.g. `make dev` or `make docker-up`). Writes JSON per scenario under ./out/member_a/
member-a-capture:
	python scripts/e2e/member_a_capture.py --out-dir ./out/member_a

# One-command Member A verification: docker up + healthz + fixture capture.
member-a-verify:
	bash scripts/e2e/member_a_verify.sh

# Member A GPU visibility check (compose GPU overlay).
member-a-gpu-check:
	bash scripts/e2e/member_a_gpu_check.sh

# Strict Task 4: API-level E2E + S3 cold roundtrip unit tests in builder container.
member-a-task4-strict:
	bash scripts/e2e/member_a_task4_strict.sh

# Unified Member A entrypoint: verify + optional GPU check + strict task4.
member-a-all:
	bash scripts/e2e/member_a_all.sh

# Production visibility/sanitization guard.
prod-safety-check:
	bash scripts/check_prod_visibility.sh

# ── Layer-1 Benchmarks ─────────────────────────────────────────────────────────
# Group 2: FAISS HNSW (pure ANN baseline — no pipeline overhead)
# Group 3: Plasmod retrievalplane direct (Go CGO → Knowhere HNSW)
# Group 4: Plasmod full system  (HTTP → full agent-native pipeline)
#
# Requires:
#   - make cpp-with-knowhere  (builds cpp/build/libplasmod_retrieval.dylib)
#   - make build              (builds bin/plasmod with -tags retrieval)
#   - pip install numpy faiss-cpu requests
#   - Plasmod server running: bash start_server.sh
#
# Python script (Groups 2, 3, 4 via HTTP):
bench-layer1:
	python3 scripts/benchmark_layer1.py
