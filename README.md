# mcpgate

[![CI](https://github.com/9OctopusDev/mcpgate/actions/workflows/ci.yml/badge.svg)](https://github.com/9OctopusDev/mcpgate/actions/workflows/ci.yml)

`mcpgate` is a standalone universal MCP gateway for explicitly exported tools. It terminates MCP 2026-07-28 and the tested MCP 2025-11-25 compatibility subset, applies OAuth and deny-by-default policy, and invokes reviewed REST, remote MCP, or supervised stdio MCP upstreams.

The gateway publishes no discovered tool automatically. It has no mandatory database, Redis, broker, hosted control plane, runtime plugin installation, or model-provider dependency.

> **Project status:** pre-release source preview. There is no stable compatibility guarantee or tagged release yet.

## Capabilities

- Presents modern and compatible legacy MCP interfaces over one reviewed tool catalog.
- Adapts reviewed OpenAPI operations, remote MCP servers, and supervised stdio MCP processes.
- Verifies OAuth resource-server claims and applies tenant, subject, client, and scope policy immediately before dispatch.
- Quarantines changed upstream contracts instead of automatically publishing discovered tools.
- Exposes loopback-only health and operational status endpoints without requiring a durable store.

Read the [technical design](docs/design/universal-mcp-gateway.md), [security boundaries](docs/security.md), and [operations guide](docs/operations.md) before deploying the gateway.

## Build and test locally

Install Go 1.27.3 and Docker, then clone the canonical repository:

```sh
git clone https://github.com/9OctopusDev/mcpgate.git
cd mcpgate
make build
make test
make conformance
```

Run the complete local release gate and build the container with:

```sh
make release-gate
make container
```

The example configuration contains documentation-only hostnames, credentials, and stdio paths. Provide safe local values before serving it. Its structure can be inspected with:

```sh
./mcpgate validate --config examples/gateway.yaml
./mcpgate inspect --config examples/gateway.yaml --format json
```

The data listener defaults in the example to `127.0.0.1:8080`; the administrative listener is loopback-only at `127.0.0.1:9090`. Do not expose the administrative listener through an ingress, Service, host port, or sidecar proxy.

## Contributing and security

See [CONTRIBUTING.md](CONTRIBUTING.md) for the development workflow. Report vulnerabilities through the private process in [SECURITY.md](SECURITY.md), never through a public issue.

mcpgate is licensed under the [Apache-2.0 license](LICENSE). See [NOTICE](NOTICE) for attribution information.
