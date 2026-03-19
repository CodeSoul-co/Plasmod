.PHONY: dev build test integration-test cpp sdk-python fmt

dev:
	go run ./src/cmd/server

build:
	go build ./src/cmd/server

cpp:
	cmake -S cpp -B build && cmake --build build

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

fmt:
	gofmt -w $(shell find src -name '*.go')
