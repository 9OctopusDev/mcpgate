# mcpgate

`mcpgate` is a standalone universal MCP gateway for explicitly exported tools. It terminates MCP 2026-07-28 and the tested MCP 2025-11-25 compatibility subset, applies OAuth and deny-by-default policy, and invokes reviewed REST, remote MCP, or supervised stdio MCP upstreams.

The gateway publishes no discovered tool automatically. It has no mandatory database, Redis, broker, hosted control plane, runtime plugin installation, or model-provider dependency.

## Build and verify

```sh
make build
make test
make conformance
make release-gate
```

The release toolchain is Go 1.27.3. Validate the example with:

```sh
mcpgate validate --config examples/gateway.yaml
mcpgate inspect --config examples/gateway.yaml --format json
```

See [operations](docs/operations.md), [security boundaries](docs/security.md), and the [release checklist](docs/release-checklist.md).
