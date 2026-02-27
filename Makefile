.PHONY: build run test lint fmt imports tidy

build:
	go build ./...

run:
	go run ./cmd/mcp-server

test:
	go test ./...

lint:
	golangci-lint run

fmt:
	go fmt ./...

imports:
	goimports -l -w .

tidy:
	go mod tidy
