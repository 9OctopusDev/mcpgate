# mcpgate operations

`mcpgate` is a Linux-first, single-process gateway. Mount one read-only `mcpgate/v1` configuration and all file-backed secrets read-only. Terminate TLS at a trusted ingress, preserve the original authority, and do not publish the admin listener. The v1 admin listener must bind loopback.

## Commands

- `mcpgate validate --config /etc/mcpgate/gateway.yaml` validates bounded YAML, references, and local OpenAPI contracts without claiming online readiness.
- `mcpgate validate --online ...` additionally requires online contracts to resolve.
- `mcpgate inspect --config ... --format json` emits the redacted export view.
- `mcpgate serve --config ...` starts the configured process. `SIGHUP` requests transactional reload; listener topology changes require restart. `SIGTERM` and `SIGINT` stop admission and begin bounded draining.
- `mcpgate healthcheck` queries only a loopback readiness URL and is suitable for the container health check.

Readiness requires an active generation and every required upstream contract. Optional unavailable upstreams remain dormant. Failed reloads preserve the active generation. A `RESTART_REQUIRED` reload result means the data/admin bind topology changed.

## Replicas and durability

Sessions, rate-limit counters, discovery freshness, in-flight correlation, and stdout audit delivery are memory-local and are lost on restart. Configure legacy-session affinity. Quotas apply per instance, not globally. Every replica must receive the same configuration, export set, credential epochs, and cursor signing key. Rolling secret rotation must retain overlap long enough for old catalog cursors and leased generations to retire.

No exactly-once guarantee exists. A connection failure after dispatch is reported as an unknown outcome. Operators must provide upstream-specific reconciliation and idempotency procedures; the gateway does not replay mutating or unknown operations.

## Stdio profile

The default image contains no child executables. Build a separate derived image that copies only reviewed absolute executables and their fixed dependencies. Stdio isolation is process partitioning, not a security sandbox. Apply container seccomp/AppArmor, read-only root filesystems, dropped capabilities, resource limits, and a dedicated service account.

## Runbook

1. Check `/livez`, then `/readyz` and `/v1/status` over loopback.
2. Inspect `/v1/upstreams` for dormant/quarantined contracts and `/v1/routes` for the active revision.
3. For unknown outcomes, stop automated retries and reconcile with the native upstream.
4. For contract quarantine, compare the discovered fingerprint with the reviewed export and deploy a reviewed configuration revision.
5. Before shutdown, remove the instance from ingress and allow the configured drain deadline.

