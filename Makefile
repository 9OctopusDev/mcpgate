GO ?= go

.PHONY: build test race vet conformance

build:
	$(GO) build ./cmd/mcpgate

test:
	$(GO) test ./... -count=1

race:
	$(GO) test -race ./... -count=1

vet:
	$(GO) vet ./...

conformance:
	$(GO) test ./tests/conformance -count=1
	$(GO) test -race ./internal/protocol/... ./internal/adapters/mcp/... -count=1
