#
# Multi-stage build for Plasmod API server (split :9091 + :19530).
#   docker build -t oneflybird/plasmod:1.21 .
#
# Notes:
# - GGUF builds use a pinned go-llama.cpp checkout only for its C binding.
# - Go is installed from the official tarball in the Debian builder (no golang:* base image).

# Build-time source switches for air-gapped/intranet environments.
# Examples:
#   docker build \
#     --build-arg BASE_REGISTRY=registry.company.local/library/ \
#     --build-arg APT_MIRROR=http://apt-mirror.company.local/debian \
#     --build-arg GO_LLAMACPP_REPO=https://git.company.local/mirror/go-llama.cpp.git \
#     --build-arg GOPROXY=https://goproxy.company.local,direct \
#     --build-arg GOSUMDB=off \
#     -t plasmod:latest .
ARG BASE_REGISTRY=
ARG DEBIAN_IMAGE=debian:bookworm-slim

# Stage 1: build plasmod-server
FROM ${BASE_REGISTRY}${DEBIAN_IMAGE} AS builder

ARG APT_MIRROR=
ARG GO_LLAMACPP_REPO=https://github.com/go-skynet/go-llama.cpp.git
ARG GO_LLAMACPP_REF=6a8041ef6b46d4712afc3ae791d1c2d73da0ad1c
ARG TARGETARCH
ARG GO_VERSION=1.25.0
# China mirror example: --build-arg GO_DOWNLOAD_BASE=https://mirrors.aliyun.com/golang
ARG GO_DOWNLOAD_BASE=https://go.dev/dl
ARG GOPROXY=
ARG GOSUMDB=
ARG ONNXRUNTIME_VERSION=1.17.3

RUN if [ -n "${APT_MIRROR}" ]; then \
      sed -i "s|http://deb.debian.org/debian|${APT_MIRROR}|g" /etc/apt/sources.list.d/debian.sources && \
      sed -i "s|http://security.debian.org/debian-security|${APT_MIRROR}|g" /etc/apt/sources.list.d/debian.sources; \
    fi && \
    apt-get update && apt-get install -y --no-install-recommends \
    build-essential \
    ca-certificates \
    cmake \
    curl \
    git \
    libgomp1 \
    libopenblas-dev \
    pkg-config \
    && rm -rf /var/lib/apt/lists/*

# Install the architecture-matched ONNX Runtime shared library for embedding inference.
RUN target_arch="${TARGETARCH:-$(dpkg --print-architecture)}"; \
    case "$target_arch" in amd64) onnx_arch=x64 ;; arm64) onnx_arch=aarch64 ;; *) echo "unsupported ONNX Runtime architecture: $target_arch" >&2; exit 1 ;; esac; \
    curl -fsSL \
    "https://github.com/microsoft/onnxruntime/releases/download/v${ONNXRUNTIME_VERSION}/onnxruntime-linux-${onnx_arch}-${ONNXRUNTIME_VERSION}.tgz" \
    | tar xz -C /tmp && \
    cp "/tmp/onnxruntime-linux-${onnx_arch}-${ONNXRUNTIME_VERSION}/lib/libonnxruntime.so.${ONNXRUNTIME_VERSION}" \
       /usr/local/lib/libonnxruntime.so && \
    ldconfig

# go.mod needs >= 1.25.0. BuildKit supplies TARGETARCH for multi-architecture
# builds; fall back to Debian's architecture when it is not set.
RUN go_arch="${TARGETARCH:-$(dpkg --print-architecture)}"; \
    case "$go_arch" in amd64|arm64) ;; *) echo "unsupported Go architecture: $go_arch" >&2; exit 1 ;; esac; \
    curl -fsSL "${GO_DOWNLOAD_BASE}/go${GO_VERSION}.linux-${go_arch}.tar.gz" \
    | tar -C /usr/local -xzf -
ENV PATH=/usr/local/go/bin:${PATH}
ENV GOTOOLCHAIN=local

# Build the C binding used by the optional GGUF embedder. The Go module itself
# is resolved from the pinned dependency in go.mod.
RUN git clone --depth 1 --recurse-submodules "${GO_LLAMACPP_REPO}" /tmp/go-llama-cpp \
    && cd /tmp/go-llama-cpp \
    && test "$(git rev-parse HEAD)" = "${GO_LLAMACPP_REF}" \
    && make -j"$(nproc)" libbinding.a

WORKDIR /src

COPY src ./src
COPY go.mod go.sum ./

# The repository intentionally does not vendor Go modules. Download after the
# local retrievalplane replacement is present in the build context.
ENV GOPROXY=${GOPROXY}
ENV GOSUMDB=${GOSUMDB}
RUN go mod download

COPY . .

# C++ warm-segment ANN (POST /v1/ingest/vectors, HNSW). Required for Milvus-style vector ingest.
RUN cmake -S cpp -B cpp/build -DCMAKE_BUILD_TYPE=Release -DANDB_KNOWHERE_FAISS=ON \
    && cmake --build cpp/build --parallel "$(nproc)" \
    && mkdir -p /src/out/lib \
    && cp cpp/build/libplasmod_retrieval.so /src/out/lib/ \
    && find cpp/build -name 'libknowhere*.so' -exec cp -t /src/out/lib {} +

ENV CGO_ENABLED=1
ENV LIBRARY_PATH=/tmp/go-llama-cpp
ENV C_INCLUDE_PATH=/tmp/go-llama-cpp
ENV CGO_CFLAGS="-I/src/cpp/include"
ENV CGO_LDFLAGS="-L/src/cpp/build -lplasmod_retrieval -Wl,-rpath,/usr/local/lib"

RUN mkdir -p /src/bin \
    && go version \
    && go build -buildvcs=false -mod=readonly -trimpath -tags retrieval \
        -ldflags="-s -w" -o /src/bin/plasmod-server ./src/cmd/server

# Stage 2: minimal runtime (no shell wrapper)
FROM ${BASE_REGISTRY}${DEBIAN_IMAGE} AS runtime

ARG APT_MIRROR=

RUN if [ -n "${APT_MIRROR}" ]; then \
      sed -i "s|http://deb.debian.org/debian|${APT_MIRROR}|g" /etc/apt/sources.list.d/debian.sources && \
      sed -i "s|http://security.debian.org/debian-security|${APT_MIRROR}|g" /etc/apt/sources.list.d/debian.sources; \
    fi && \
    apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    libc6-dev \
    libgomp1 \
    libopenblas0 \
    libstdc++6 \
    && rm -rf /var/lib/apt/lists/*

# ONNX Runtime (embedding) + C++ retrieval bridge (warm HNSW / ingest_vectors).
COPY --from=builder /usr/local/lib/libonnxruntime.so /usr/local/lib/libonnxruntime.so
COPY --from=builder /src/out/lib/ /usr/local/lib/
RUN ldconfig

COPY --from=builder /src/bin/plasmod-server /usr/local/bin/plasmod-server

ENV PLASMOD_LISTEN_MODE=split
ENV PLASMOD_MGMT_ADDR=0.0.0.0:9091
ENV PLASMOD_API_ADDR=0.0.0.0:19530
ENV PLASMOD_SHOW_BANNER=1
ENV PLASMOD_PUBLIC_HOST=127.0.0.1
EXPOSE 9091 19530

ENTRYPOINT ["/usr/local/bin/plasmod-server"]
