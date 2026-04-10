#
# Multi-stage build for CogDB / ANDB API server (README Member A task 1).
#   docker build -t cogdb:latest .
#
# Notes:
# - go.mod replaces github.com/go-skynet/go-llama.cpp with /tmp/go-llama-cpp.
# - In this environment, executing /bin/sh inside golang:1.24-bookworm failed.
#   We therefore copy the Go toolchain out of that image and run all build steps
#   in a Debian builder stage where shell commands work.

# Build-time source switches for air-gapped/intranet environments.
# Examples:
#   docker build \
#     --build-arg BASE_REGISTRY=registry.company.local/library/ \
#     --build-arg APT_MIRROR=http://apt-mirror.company.local/debian \
#     --build-arg GO_LLAMACPP_REPO=https://git.company.local/mirror/go-llama.cpp.git \
#     --build-arg GOPROXY=https://goproxy.company.local,direct \
#     --build-arg GOSUMDB=off \
#     -t cogdb:latest .
ARG BASE_REGISTRY=
ARG GOLANG_IMAGE=golang:1.24-bookworm
ARG DEBIAN_IMAGE=debian:bookworm-slim

# Stage 0: provide Go toolchain files (do not execute commands here)
FROM ${BASE_REGISTRY}${GOLANG_IMAGE} AS go-toolchain

# Stage 1: build andb-server
FROM ${BASE_REGISTRY}${DEBIAN_IMAGE} AS builder

ARG APT_MIRROR=
ARG GO_LLAMACPP_REPO=https://github.com/go-skynet/go-llama.cpp.git
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
    pkg-config \
    && rm -rf /var/lib/apt/lists/*

# Install ONNX Runtime shared library (CPU) for embedding inference.
RUN curl -fsSL \
    "https://github.com/microsoft/onnxruntime/releases/download/v${ONNXRUNTIME_VERSION}/onnxruntime-linux-x64-${ONNXRUNTIME_VERSION}.tgz" \
    | tar xz -C /tmp && \
    cp /tmp/onnxruntime-linux-x64-${ONNXRUNTIME_VERSION}/lib/libonnxruntime.so.${ONNXRUNTIME_VERSION} \
       /usr/local/lib/libonnxruntime.so && \
    ldconfig

# Copy Go runtime/toolchain from official Go image.
COPY --from=go-toolchain /usr/local/go /usr/local/go
ENV PATH=/usr/local/go/bin:${PATH}

# Satisfy go.mod local replace for go-llama.cpp.
RUN git clone --depth 1 --recurse-submodules "${GO_LLAMACPP_REPO}" /tmp/go-llama-cpp \
    && cd /tmp/go-llama-cpp \
    && make -j"$(nproc)" libbinding.a

WORKDIR /src

# go.mod replace: andb/retrievalplane => ./src/internal/dataplane/retrievalplane
# go.mod also replaces github.com/go-skynet/go-llama.cpp => ./libs/go-llama.cpp.
# Prepare that local path inside the build container before `go mod download`.
RUN mkdir -p /src/libs \
    && cp -a /tmp/go-llama-cpp /src/libs/go-llama.cpp

COPY go.mod go.sum ./
COPY src ./src
RUN if [ -n "${GOPROXY}" ]; then go env -w GOPROXY="${GOPROXY}"; fi \
    && if [ -n "${GOSUMDB}" ]; then go env -w GOSUMDB="${GOSUMDB}"; fi \
    && go mod download

COPY . .

ENV CGO_ENABLED=1
ENV LIBRARY_PATH=/tmp/go-llama-cpp
ENV C_INCLUDE_PATH=/tmp/go-llama-cpp

RUN mkdir -p /src/bin \
    && go build -buildvcs=false -trimpath -ldflags="-s -w" -o /src/bin/andb-server ./src/cmd/server

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
    && rm -rf /var/lib/apt/lists/*

# Copy ONNX Runtime library from builder stage.
COPY --from=builder /usr/local/lib/libonnxruntime.so /usr/local/lib/libonnxruntime.so
RUN ldconfig

COPY --from=builder /src/bin/andb-server /usr/local/bin/andb-server

ENV ANDB_HTTP_ADDR=0.0.0.0:8080
EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/andb-server"]
