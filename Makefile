GO ?= go

.PHONY: build test race vet conformance fuzz-seeds public-check release-gate container

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

fuzz-seeds:
	$(GO) test ./internal/config ./internal/catalog ./internal/adapters/rest ./internal/protocol/modern -run '^Fuzz' -count=1

public-check:
	bash scripts/check-public-repo.sh

release-gate: test race conformance vet fuzz-seeds
	$(GO) test ./tests/faults -count=1
	$(GO) test ./tests/benchmarks -run '^$$' -bench BenchmarkGateway -benchtime=5s

container:
	docker build --pull=false -t mcpgate:local .
