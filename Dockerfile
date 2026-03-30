#
# Multi-stage build for CogDB / ANDB API server (README Member A task 1).
#   docker build -t cogdb:latest .
#
# Notes:
# - go.mod replaces github.com/go-skynet/go-llama.cpp with /tmp/go-llama-cpp.
# - In this environment, executing /bin/sh inside golang:1.24-bookworm failed.
#   We therefore copy the Go toolchain out of that image and run all build steps
#   in a Debian builder stage where shell commands work.

# Stage 0: provide Go toolchain files (do not execute commands here)
FROM golang:1.24-bookworm AS go-toolchain

# Stage 1: build andb-server
FROM debian:bookworm-slim AS builder

RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential \
    ca-certificates \
    cmake \
    git \
    pkg-config \
    && rm -rf /var/lib/apt/lists/*

# Copy Go runtime/toolchain from official Go image.
COPY --from=go-toolchain /usr/local/go /usr/local/go
ENV PATH=/usr/local/go/bin:${PATH}

# Satisfy go.mod local replace for go-llama.cpp.
RUN git clone --depth 1 --recurse-submodules https://github.com/go-skynet/go-llama.cpp.git /tmp/go-llama-cpp \
    && cd /tmp/go-llama-cpp \
    && make -j"$(nproc)" libbinding.a

WORKDIR /src

# go.mod replace: andb/retrievalplane => ./src/internal/dataplane/retrievalplane
COPY go.mod go.sum ./
COPY src ./src
RUN go mod download

COPY . .

ENV CGO_ENABLED=1
ENV LIBRARY_PATH=/tmp/go-llama-cpp
ENV C_INCLUDE_PATH=/tmp/go-llama-cpp

RUN mkdir -p /src/bin \
    && go build -buildvcs=false -trimpath -ldflags="-s -w" -o /src/bin/andb-server ./src/cmd/server

# Stage 2: minimal runtime (no shell wrapper)
FROM debian:bookworm-slim AS runtime

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    libc6-dev \
    && rm -rf /var/lib/apt/lists/*

COPY --from=builder /src/bin/andb-server /usr/local/bin/andb-server

ENV ANDB_HTTP_ADDR=0.0.0.0:8080
EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/andb-server"]
