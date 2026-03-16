.PHONY: dev build test cpp sdk-python fmt

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

fmt:
	gofmt -w $(shell find src -name '*.go')
