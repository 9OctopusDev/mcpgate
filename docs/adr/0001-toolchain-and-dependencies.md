# ADR 0001: Toolchain and dependency boundary

## Status

Accepted on 2026-10-20.

## Decision

Build and release with Go 1.27.3. This supersedes the specification's 1.27.2 baseline with the next patch release containing a `crypto/x509` security fix.

Pin direct dependencies to MCP Go SDK v1.8.0, libopenapi v0.41.6, santhosh-tekuri/jsonschema v6.0.3, goccy/go-yaml v1.19.2, lestrrat-go/jwx v4.1.0, Prometheus client_golang v1.24.1, and OpenTelemetry Go v1.47.0.

MCP SDK types may appear only in `internal/protocol` and `internal/adapters/mcp`. All other packages communicate with gateway-owned types from `internal/contracts`. OpenAPI parsing does not grant network or filesystem authority; the compiler supplies bounded local bytes and operator-selected destinations.

## Consequences

Patch-level Go upgrades require the full race, conformance, vulnerability, and release qualification gates. Dependency upgrades require protocol and trust-boundary review rather than an automated floating-version merge.

