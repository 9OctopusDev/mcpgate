# Release checklist

- [ ] `make release-gate` passes with Go 1.27.3.
- [ ] `go test -race ./... -count=1` passes on the release platform.
- [ ] `govulncheck ./...` has no untriaged finding.
- [ ] `tests/release-cases.json` contains every CFG, REST, MCP, AUTH, and REL identifier exactly once.
- [ ] Modern and legacy golden wire tests pass independently.
- [ ] Fuzz seeds pass; scheduled fuzz jobs complete without panic, ambiguity acceptance, escape, or disclosure.
- [ ] Benchmark environment and p95 gateway-added latency are recorded.
- [ ] The nonroot, read-only container starts with read-only config/secret mounts and its admin port is not published.
- [ ] Replica configuration, cursor keys, secret epochs, legacy affinity, and reconciliation ownership are documented for the deployment.
- [ ] The release report records non-durable sessions, counters, discovery freshness, audit stdout, and absence of exactly-once execution.

