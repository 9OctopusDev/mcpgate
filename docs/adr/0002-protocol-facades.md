# ADR 0002: Isolated MCP protocol facades

## Status

Accepted, 2026-10-20.

## Decision

`mcpgate` implements MCP 2026-07-28 and MCP 2025-11-25 in separate packages. The modern facade requires request metadata and uses request-scoped streaming. The legacy facade owns initialization state, bounded sessions, detached POST execution, and session-scoped cancellation. Both consume gateway-owned contracts and the common executor.

Remote MCP wire and SDK concerns are confined to `internal/adapters/mcp`; downstream wire concerns are confined to `internal/protocol`. No MCP SDK type may cross into configuration, policy, routing, execution, runtime, or telemetry packages.

## Consequences

- Cross-era translation is explicit and testable rather than accidental shared-handler behavior.
- Unsupported resources, prompts, sampling, elicitation, roots, tasks, and subscriptions fail as unknown methods.
- Upgrading one protocol era requires its own golden wire and lifecycle review.
- Legacy clients receive only contracts whose output shape is representable without schema rewriting.

