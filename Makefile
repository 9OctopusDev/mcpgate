GO ?= go

.PHONY: build test race vet

build:
	$(GO) build ./cmd/mcpgate

test:
	$(GO) test ./... -count=1

race:
	$(GO) test -race ./... -count=1

vet:
	$(GO) vet ./...

