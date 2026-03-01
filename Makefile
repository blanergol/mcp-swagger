.PHONY: build run test lint fmt imports tidy

build:
	go build ./...

run:
	go run ./cmd/mcp-server

test:
	go test -covermode=atomic -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out

lint:
	golangci-lint run

fmt:
	go fmt ./...

imports:
	goimports -l -w .

tidy:
	go mod tidy
