# Universal MCP Gateway v1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build `mcpgate`, a standalone Linux-first Go gateway that terminates modern and legacy MCP downstream, authorizes explicit tool exports, and invokes REST, remote MCP, or supervised stdio MCP upstreams without requiring changes to upstream applications.

**Architecture:** Compile strict YAML, local OpenAPI documents, discovered MCP manifests, policies, credentials, and transport resources into immutable generations published through a leased snapshot store. The data plane uses separate modern and legacy protocol facades over a common authenticate-route-authorize-validate-admit-dispatch-normalize lifecycle; gateway-owned contracts isolate the rest of the program from MCP SDK types. Control-plane discovery, refresh, and reload have separate budgets from request execution, while live revocation/quarantine overlays can stop queued work without mutating leased generations.

**Tech Stack:** Go 1.27.3 release toolchain (the specification's 1.27.2 floor plus the October 19 security patch), `net/http`, MCP Go SDK v1.8.0, libopenapi v0.41.6, santhosh-tekuri/jsonschema v6.0.3, goccy/go-yaml v1.19.2, lestrrat-go/jwx v4.1.0, Prometheus client_golang v1.24.1, OpenTelemetry Go v1.47.0, JSON Schema 2020-12, Docker.

**Spec:** `universal-mcp-gateway-implementation.md`

## Global Constraints

- Target one standalone `mcpgate` binary and container, Linux first; do not add a mandatory database, Redis, message broker, hosted control plane, model provider, runtime plugin system, or package installation.
- Implement MCP `2026-07-28` as the primary contract and `2025-11-25` as the explicitly tested legacy contract; keep their lifecycle, transport, result, cancellation, and session behavior in separate facades.
- Pin Go 1.27.3 for builds because it supersedes the specification's Go 1.27.2 baseline with a `crypto/x509` security fix; record this narrow baseline amendment in an ADR before implementation.
- Pin direct dependencies to MCP Go SDK v1.8.0, libopenapi v0.41.6, jsonschema v6.0.3, goccy/go-yaml v1.19.2, jwx v4.1.0, client_golang v1.24.1, and OpenTelemetry Go v1.47.0; commit `go.sum`, run vulnerability scanning, and never use `@latest` in release commands.
- v1 advertises tools only. Resources, prompts, elicitation, sampling, roots, tasks, subscriptions/list-change delivery, and interactive bridging remain disabled and must be rejected rather than partially emulated.
- Publish only explicit exports with exact public names. Never auto-publish discovery additions, split public names to infer an upstream, expose a generic URL tool, or trust upstream annotations as authorization/retry policy.
- Configuration is one bounded UTF-8 YAML document. Reject duplicate keys, unknown fields, custom tags, excessive aliases/nesting, source-root/symlink escapes, remote OpenAPI documents, and external schema references.
- Downstream and upstream credentials are separate security domains. Build principals only from validated claims; never forward downstream authorization, cookies, host/forwarding headers, session identifiers, or raw JSON-RPC identifiers upstream.
- Default to one explicit execution attempt. Only operator-reviewed `read_only` routes may exceed one gateway-controlled attempt; never automatically replay `mutating` or `unknown` work.
- Initial limits are 256 active requests, 64 queued requests, 250 ms queue wait, 30 s ordinary call timeout, 1 MiB input, 4 MiB encoded result, 2 MiB source schema, 256 MiB shared buffer budget, 20 requests/s with burst 40 per principal, 8 discovery jobs, 64 nesting levels, 10,000 schema nodes, 1,000 legacy sessions, and 15-minute legacy idle expiry.
- Listener authority and path routing are exact. Validate host/origin before protocol handling, preserve endpoint base paths, validate resolved IPs immediately before dialing, retain TLS server-name verification, and disable redirects and environment proxies by default.
- A generation is immutable after publication. Every invocation, stream, detached legacy execution, session, and stdio process has explicit lease ownership and exactly one idempotent release path.
- Logs, metrics, traces, diagnostics, revisions, CLI output, and child stderr must not disclose secrets, tokens, raw arguments/results, session IDs, continuation data, or low-entropy identity hashes.

## Scope and Delivery Shape

The specification covers tightly coupled but independently reviewable subsystems. Implement it as five shippable milestones; each milestone ends in working, testable software, but only Milestone 5 qualifies as the v1 product.

1. **Foundation:** Tasks 1-5 produce strict configuration, gateway contracts, policy/identity, immutable generations, and local routing/catalogs.
2. **REST vertical slice:** Tasks 6-9 produce a modern MCP-to-REST path with bounded execution, results, progress, audit, and security controls.
3. **Remote MCP and compatibility:** Tasks 10-12 add remote MCP discovery/invocation and legacy downstream/upstream behavior.
4. **Stdio and operations:** Tasks 13-15 add process supervision, reload/refresh/lifecycle, CLI, administration, and telemetry.
5. **Release qualification:** Task 16 closes every named acceptance case, fuzz/race/security gates, packaging, and performance evidence.

Do not market Milestone 2 as the universal gateway. Production v1 requires REST, remote MCP, stdio MCP, and the declared legacy subset.

## Repository and File Map

The repository currently contains only the specification; the reference kit named in Section 25 is absent. Task 1 reconstructs the schema, example, fixtures, and gateway-owned contracts as versioned source files.

| Path | Responsibility |
| --- | --- |
| `cmd/mcpgate/main.go` | Process entry point and exit-code mapping only. |
| `internal/app/app.go` | CLI composition, dependency wiring, serve/validate/inspect/version commands. |
| `internal/contracts/*.go` | Dependency-free route, principal, catalog, call, outcome, event, and adapter contracts. |
| `internal/config/*.go` | Strict bounded YAML loading, source paths, defaults, structural types, secret references. |
| `internal/compiler/*.go` | Semantic checks, deterministic revisions, candidate construction, diagnostics. |
| `internal/runtime/*.go` | Snapshot leases/publication, resource retirement, revocation overlay, lifecycle coordination. |
| `internal/identity/*.go` | Protected-resource metadata, JWT/JWKS verification, trusted `Principal` construction. |
| `internal/credentials/*.go` | Env/file secret resolution, opaque bindings and epochs, redaction. |
| `internal/policy/*.go` | Deny-by-default authorization and dispatch-time epoch recheck. |
| `internal/router/*.go` | Exact host/path and public-name lookup. |
| `internal/catalog/*.go` | Authorized views, protocol-compatible filtering, signed cursors, fingerprints. |
| `internal/adapters/rest/*.go` | OpenAPI normalization, `RESTPlan`, URL/query/body construction, response validation/envelopes. |
| `internal/adapters/mcp/*.go` | SDK-contained discovery, protocol translation, invocation, refresh and quarantine. |
| `internal/upstream/*.go` | Safe HTTP transports, DNS/network policy, endpoint health, weighted selection, circuits. |
| `internal/supervisor/*.go` | Stdio subprocess partitions, framing, backoff, drain/reap lifecycle. |
| `internal/executor/*.go` | Ordered call state machine, admission, deadlines, retries, cancellation, cleanup. |
| `internal/edge/*.go` | Public HTTP limits, authority/origin validation, exact virtual-server dispatch. |
| `internal/protocol/modern/*.go` | MCP 2026-07-28 codec/handler and request-scoped SSE behavior. |
| `internal/protocol/legacy/*.go` | MCP 2025-11-25 initialization, sessions, cancellation and result compatibility. |
| `internal/admin/*.go` | Loopback health/status/routes/upstreams/metrics handlers. |
| `internal/telemetry/*.go` | Structured redacted events, bounded metric labels, trace spans. |
| `schemas/gateway-config.v1.schema.json` | Authoritative v1 structural configuration language. |
| `examples/gateway.yaml` | Complete runnable-shape configuration copied from the spec with placeholder endpoints. |
| `testdata/{config,openapi,protocol,stdio}/` | Deterministic positive, hostile, and cross-version fixtures. |
| `internal/testkit/*.go` | Injected clock, recording HTTP/MCP upstreams, controllable dialer, fake child process. |
| `tests/{integration,conformance,faults,benchmarks}/` | Cross-package release gates mapped to Section 23 IDs. |
| `docs/adr/*.md` | Toolchain/dependency, protocol-facade, identity, and retry decisions. |
| `docs/operations.md`, `docs/security.md` | Deployment, limitations, runbook, and threat-boundary documentation. |
| `Dockerfile`, `Makefile`, `.github/workflows/ci.yml` | Reproducible build, checks, container and CI gates. |

## Review Focus

These five implied failure classes receive explicit regression tests in their owning tasks:

1. A JSON object with duplicate `method`, `name`, or `_meta` members must be rejected before route selection or upstream calls (Task 9).
2. A permitted hostname that resolves to an allowed address and then rebinds to link-local/private space must be blocked at dial time without losing TLS hostname verification (Task 4).
3. Two authorization-epoch changes during one catalog serialization must produce one bounded retryable failure, not an infinite restart loop or a mixed-policy page (Task 5).
4. Authentication/policy failure discovered after an SSE response starts must be encoded as a permitted terminal protocol/result event or closed safely, never as an impossible second HTTP status (Task 9).
5. Cancelling or timing out one call on a concurrency-qualified shared stdio child must not kill unrelated calls; if a single-inflight child hangs, only that process partition is terminated after grace (Task 13).

---

### Task 1: Bootstrap the Module and Restore the Reference Kit

**Files:**
- Create: `go.mod`, `go.sum`, `Makefile`, `.gitignore`
- Create: `cmd/mcpgate/main.go`, `internal/app/app.go`, `internal/contracts/types.go`, `internal/contracts/outcome.go`
- Create: `schemas/gateway-config.v1.schema.json`, `examples/gateway.yaml`
- Create: `testdata/openapi/users.openapi.yaml`, `testdata/protocol/tools-call.modern.http`, `testdata/protocol/tools-result.modern.json`
- Create: `docs/adr/0001-toolchain-and-dependencies.md`
- Test: `internal/contracts/outcome_test.go`, `tests/integration/reference_assets_test.go`

**Interfaces:**
- Consumes: the configuration, sample operation, protocol examples, and contracts embedded in `universal-mcp-gateway-implementation.md`.
- Produces: `contracts.RouteKey`, `Route`, `ToolContract`, `Principal`, `CredentialBinding`, `Catalog`, `Call`, `Outcome`, `EventSink`, `ToolAdapter`, `SnapshotStore`, and `Authorizer`; a compilable module pinned to the versions in Global Constraints.

- [ ] **Step 1: Initialize version control and the pinned Go module**

Run:

```bash
git init
go mod init github.com/mcpgate/mcpgate
go get github.com/modelcontextprotocol/go-sdk@v1.8.0
go get github.com/pb33f/libopenapi@v0.41.6
go get github.com/santhosh-tekuri/jsonschema/v6@v6.0.3
go get github.com/goccy/go-yaml@v1.19.2
go get github.com/lestrrat-go/jwx/v4@v4.1.0
go get github.com/prometheus/client_golang@v1.24.1
go get go.opentelemetry.io/otel@v1.47.0
```

Expected: `go.mod` declares `go 1.27.3`; every direct dependency has the exact requested version and `go.sum` exists.

- [ ] **Step 2: Write failing ownership-invariant tests for `Outcome`**

Create tests that construct zero, one, and two variants and assert only the single-variant case succeeds:

```go
func TestOutcomeValidateRequiresExactlyOneVariant(t *testing.T) {
    complete := contracts.Outcome{Complete: &contracts.ToolResult{Structured: json.RawMessage(`{"ok":true}`)}}
    if err := complete.Validate(); err != nil { t.Fatal(err) }
    if err := (contracts.Outcome{}).Validate(); err == nil { t.Fatal("empty outcome accepted") }
    both := complete
    both.ProtocolError = &contracts.ProtocolError{Code: -32603, Message: "failure"}
    if err := both.Validate(); err == nil { t.Fatal("ambiguous outcome accepted") }
}
```

- [ ] **Step 3: Implement the dependency-free gateway contracts**

Define JSON-preserving arguments and explicit result variants:

```go
type ToolAdapter interface {
    Discover(context.Context, CredentialBinding) (Catalog, error)
    CallTool(context.Context, Call, EventSink) (Outcome, error)
    Close(context.Context) error
}

type Outcome struct {
    Complete      *ToolResult
    ProtocolError *ProtocolError
    InputRequired *InputRequired
}

func (o Outcome) Validate() error // count non-nil variants; require exactly one
```

Copy mutable `[]byte`/`json.RawMessage` values in constructors and keep all MCP SDK imports out of `internal/contracts`.

- [ ] **Step 4: Restore and validate the reference assets**

Copy the full YAML example and `getUser` OpenAPI operation from the specification into their named files. Encode every v1 field in the JSON Schema with `additionalProperties: false`; constrain advanced feature flags to `false`; include all required enums and minimums. Add a test that parses the schema, validates `examples/gateway.yaml`, parses the OpenAPI document, and checks that the modern HTTP fixture contains both body metadata and mirror headers.

- [ ] **Step 5: Add the command shell and build metadata**

Use `flag.FlagSet` subcommands for `validate`, `inspect`, `serve`, and `version`; wire unimplemented commands to deterministic exit code 2 with a safe message. Add ldflags variables `Version`, `Commit`, `BuildGoVersion`, and exact protocol/dependency reporting to `version --json`.

- [ ] **Step 6: Run the foundation checks**

Run:

```bash
go test ./internal/contracts ./tests/integration -run 'Outcome|ReferenceAssets' -count=1
go vet ./...
```

Expected: all tests pass and no MCP SDK type is imported outside `internal/protocol` or `internal/adapters/mcp` (enforce with an `rg` assertion in the asset test).

- [ ] **Step 7: Commit**

```bash
git add go.mod go.sum Makefile .gitignore cmd internal/contracts schemas examples testdata docs/adr/0001-toolchain-and-dependencies.md tests/integration/reference_assets_test.go
git commit -m "chore: bootstrap mcpgate contracts and reference assets"
```

### Task 2: Strict Configuration Loading and Diagnostics

**Files:**
- Create: `internal/config/model.go`, `internal/config/load.go`, `internal/config/source.go`, `internal/config/defaults.go`, `internal/config/diagnostic.go`
- Create: `testdata/config/{duplicate-key,unknown-field,custom-tag,alias-bomb,valid}.yaml`
- Test: `internal/config/load_test.go`, `internal/config/source_test.go`

**Interfaces:**
- Consumes: `schemas/gateway-config.v1.schema.json`.
- Produces: `config.Load(ctx context.Context, path string, limits SourceLimits) (*Document, []Diagnostic)` and `config.ReadLocal(baseDir, requested string, roots []string, maxBytes int64) ([]byte, SourceRef, error)`.

- [ ] **Step 1: Write failing strict-decoder tests for CFG-01 and CFG-04**

Cover one UTF-8 document, duplicate keys, unknown fields, custom tags, multiple documents, alias count/depth, byte limits, relative paths, and a symlink escaping an allowed root. Assert each diagnostic includes `Code`, `File`, `Line`, `Column`, `ConfigPath`, and a safe message.

- [ ] **Step 2: Define exact configuration structs and defaults**

Model `Gateway`, `Limits`, `Lifecycle`, `SecretRef`, `Authentication`, `Policy`, tagged `Upstream`, `Server`, `Export`, and `Observability`. Use duration wrapper types with strict textual parsing. Apply only the defaults listed in Global Constraints; reject zero/negative values where they cannot represent a usable budget.

- [ ] **Step 3: Implement bounded YAML AST validation before decoding**

Parse to an AST, walk at most 64 levels and a fixed alias/node budget, reject non-core tags and duplicate mapping keys with locations, then decode with known-field enforcement. Convert the AST to JSON and validate it against the embedded v1 schema before constructing `Document`.

- [ ] **Step 4: Implement source-root and symlink-safe reads**

Resolve ordinary relative paths against the configuration directory. Open the file, check its canonical path remains under one canonical configured root, reject non-regular files, and read with limit-plus-one. Never use the process working directory as an implicit base.

- [ ] **Step 5: Run focused tests**

Run:

```bash
go test ./internal/config -run 'Strict|Source|Symlink|Bounds|Diagnostic' -count=1
```

Expected: CFG-01 and CFG-04 cases fail before network access, while the valid example returns a fully defaulted document.

- [ ] **Step 6: Commit**

```bash
git add internal/config testdata/config
git commit -m "feat: add strict bounded configuration loading"
```

### Task 3: Credentials, Identity, Policy, and Safe Network Boundaries

**Files:**
- Create: `internal/credentials/{resolver,binding,redact}.go`
- Create: `internal/identity/{principal,jwks,verifier,metadata}.go`
- Create: `internal/policy/{compile,authorize,overlay}.go`
- Create: `internal/upstream/{network,dialer,transport}.go`
- Test: corresponding `*_test.go` files and `tests/integration/oauth_test.go`

**Interfaces:**
- Consumes: `config.SecretRef`, authentication/policy/network declarations.
- Produces: `credentials.Resolve(context.Context, config.SecretRef) (contracts.CredentialBinding, error)`, `identity.Verifier.Verify(context.Context, rawToken, resourceURI string) (contracts.Principal, error)`, `policy.Engine.Authorize(...)`, and `upstream.NewTransport(NetworkPolicy) *http.Transport`.

- [ ] **Step 1: Write failing credential and redaction tests**

Assert env/file providers only, 32-byte decoded cursor keys, file permission checks, no secret material in `%v`, `%+v`, JSON, diagnostics, revision input, or panic recovery, and a new opaque epoch after rotation without mutating active bindings.

- [ ] **Step 2: Implement immutable credential bindings**

Store secret bytes in an unexported owned buffer, expose only operation-specific header/sign/verify methods, and identify a binding by secret reference plus monotonic epoch. Add `Close` to zero owned buffers best-effort and make logs return only provider/name/epoch.

- [ ] **Step 3: Write and implement OAuth/JWKS verification tests**

Using a local rotating JWKS fixture, reject wrong issuer/audience, missing signature, disallowed algorithm, expired/not-yet-valid tokens, token-supplied key URLs, and malformed tenant/scope claims. Produce `Principal{Issuer, Subject, Tenant, ClientID, Scopes, PartitionDigest}` only after all checks pass; serve protected-resource metadata per virtual server and generate its 401 challenge.

- [ ] **Step 4: Implement deny-by-default policy and epochs**

Compile each named policy into ANDed tenant/subject/scope predicates. `Authorize` and `RecheckAtDispatch` return a decision containing the observed policy epoch. A missing policy, empty wildcard fallback, or stale revoked principal/export/upstream must deny.

- [ ] **Step 5: Write the DNS rebinding regression test**

Use a scripted resolver that returns an allowed public address during preparation and `169.254.169.254` at dial time. Assert the dial is blocked, no connection occurs, and a valid allowed address is dialed directly while `TLSClientConfig.ServerName` remains the configured hostname.

- [ ] **Step 6: Implement the controlled transport**

Resolve immediately before dial, validate every IP against hostname/CIDR policy, set the validated address on `DialContext`, preserve the hostname for TLS, disable redirects, cookies, environment proxies, and ambient auth, and create fresh request headers per call.

- [ ] **Step 7: Run security-focused tests**

Run:

```bash
go test ./internal/credentials ./internal/identity ./internal/policy ./internal/upstream ./tests/integration -run 'Credential|JWT|JWKS|Policy|DNS|OAuth' -count=1
```

Expected: AUTH-01, AUTH-03, AUTH-06, and AUTH-10 behaviors pass.

- [ ] **Step 8: Commit**

```bash
git add internal/credentials internal/identity internal/policy internal/upstream tests/integration/oauth_test.go
git commit -m "feat: establish identity policy and network trust boundaries"
```

### Task 4: Semantic Compiler and Immutable Runtime Generations

**Files:**
- Create: `internal/compiler/{compiler,validate,revision,candidate}.go`
- Create: `internal/runtime/{generation,store,lease,resources}.go`
- Test: `internal/compiler/compiler_test.go`, `internal/runtime/store_test.go`

**Interfaces:**
- Consumes: `*config.Document`, resolved credentials, adapter factories, discovered manifests.
- Produces: `compiler.Compile(context.Context, Inputs) (*runtime.Generation, []config.Diagnostic)`, `runtime.Store.Acquire() (*runtime.Lease, error)`, `runtime.Store.Publish(context.Context, *Generation) error`, and `Lease.Release()`.

- [ ] **Step 1: Write failing semantic diagnostics tests for CFG-02, CFG-03, CFG-07, CFG-08, and CFG-10**

Use table tests for missing references; duplicate path, public name, endpoint ID, or operation ID; URL credentials/query/fragment; non-HTTPS remote endpoints; allowlist mismatches; `implicit_session` exposed to modern clients; unsupported feature flags; unsafe retries; and unresolved OpenAPI security. Assert secrets never occur in diagnostics or revisions.

- [ ] **Step 2: Implement deterministic semantic compilation**

Resolve all named references, build exact route keys, check state/protocol/retry/security combinations, sort every map-derived sequence, canonicalize normalized inputs, and compute a SHA-256 revision over public contracts plus secret reference/version identifiers—not secret bytes.

- [ ] **Step 3: Write failing lease/publication race tests**

Test acquire versus publish, idempotent double release, failed candidate disposal, long-held lease retirement, shared resource reference counts, and drain-deadline cancellation. Run the test under `-race` with thousands of acquire/publish iterations.

- [ ] **Step 4: Implement the locked acquire/publish critical sections**

Use one store RW lock only for accepting/refcount/publication state. Never call network, parsing, adapter close, or subprocess shutdown while holding it. Retire the old generation after the pointer swap and close resources only after generation and shared-resource counts reach zero or the enforced drain deadline expires.

- [ ] **Step 5: Run compiler and runtime tests**

Run:

```bash
go test -race ./internal/compiler ./internal/runtime -run 'Semantic|Revision|Lease|Publish|Retire' -count=1
```

Expected: deterministic revisions, no mixed generation, no use-after-close, and CFG-02/03/07/08/10 behavior.

- [ ] **Step 6: Commit**

```bash
git add internal/compiler internal/runtime
git commit -m "feat: compile immutable leased runtime generations"
```

### Task 5: Exact Routing, Authorized Catalogs, Cursors, and Replica Pools

**Files:**
- Create: `internal/router/router.go`
- Create: `internal/catalog/{view,cursor,fingerprint}.go`
- Create: `internal/upstream/{selector,circuit,health}.go`
- Test: `internal/router/router_test.go`, `internal/catalog/{view,cursor}_test.go`, `internal/upstream/selector_test.go`

**Interfaces:**
- Consumes: immutable generation routes, `contracts.Principal`, policy engine, quarantine overlay, endpoint manifests.
- Produces: `router.Resolve(host, path, publicName string) (contracts.Route, bool)`, `catalog.List(context.Context, ListRequest) (ListPage, error)`, and `upstream.Pool.Select(affinityKey string) (*Endpoint, error)`.

- [ ] **Step 1: Test exact routing and concealment**

Assert exact host/path/public-name matching, no trailing-slash redirect, no dot parsing in `issues.search`, and identical safe unknown-tool responses for absent versus unauthorized names while internal audit causes differ.

- [ ] **Step 2: Implement effective catalog filtering**

Intersect explicit exports, valid native contracts, protocol/features, authorization, and live quarantine/revocation. Sort exact public names lexicographically, filter before pagination, materialize an immutable request-local view, and use modern `ttlMs: 0` plus `cacheScope: private`.

- [ ] **Step 3: Test and implement signed cursor binding**

Use a bounded versioned payload containing server ID, authorization partition digest, catalog revision, last name, expiry, and key ID. HMAC-sign it with the resolved cursor key. Reject altered, expired, cross-tenant, cross-server, old-revision, or oversized cursors with invalid parameters.

- [ ] **Step 4: Add the repeated authorization-epoch regression test**

Change the epoch once during serialization and assert one restart. Change it again during the retry and assert a bounded retryable failure; verify no mixed-policy page is returned and the call count is exactly two.

- [ ] **Step 5: Implement endpoint selection and affinity**

Use smooth weighted round robin across compatible healthy endpoints, per-endpoint inflight/circuit state, `CLOSED -> OPEN -> HALF_OPEN -> CLOSED`, and sticky legacy affinity. Quarantine remote MCP replicas whose fingerprints differ; count transport/availability failures but not ordinary business errors.

- [ ] **Step 6: Run catalog and routing tests**

Run:

```bash
go test ./internal/router ./internal/catalog ./internal/upstream -run 'Exact|Conceal|Cursor|Epoch|Weighted|Circuit|Affinity' -count=1
```

Expected: AUTH-02, AUTH-05, MCP-07, MCP-08, and MCP-14 behavior.

- [ ] **Step 7: Commit**

```bash
git add internal/router internal/catalog internal/upstream
git commit -m "feat: add authorized catalogs routing and endpoint pools"
```

### Task 6: Compile the Supported OpenAPI Subset into REST Plans

**Files:**
- Create: `internal/adapters/rest/{plan,openapi,schema,parameters,security}.go`
- Create: `testdata/openapi/{collisions,serialization,nullable,composition,external-ref,recursive,security}.yaml`
- Test: `internal/adapters/rest/compiler_test.go`

**Interfaces:**
- Consumes: bounded local OpenAPI 3.0.x/3.1.x bytes, selected `operationId`, operator endpoints and credential bindings.
- Produces: immutable `rest.RESTPlan` with method, path segments, parameter serializers, body/response validators, selected security alternative, limits, and retry classification.

- [ ] **Step 1: Write failing compiler tests for REST-02, REST-05, and REST-12**

Cover duplicate/absent operation IDs; path simple scalars; query form scalar/flat-array explode on/off; rejected query objects/deepObject/matrix/label/spaceDelimited/pipeDelimited/header/cookie parameters; JSON-only request bodies; rejected multipart/binary/callback/webhook mappings; and conjunctive versus alternative security requirements.

- [ ] **Step 2: Implement bounded document parsing and operation selection**

Configure libopenapi with no remote lookup, document-local references only, source byte limit, maximum depth 64, and maximum 10,000 subschemas. Ignore document `servers`; bind every plan only to operator endpoints.

- [ ] **Step 3: Implement 3.0-to-2020-12 normalization and 3.1 preservation**

Convert OpenAPI 3.0 `nullable` and exclusive bounds explicitly, preserve `allOf`/`anyOf`/`oneOf`, remove response-only request properties only when unambiguous, retain 3.1 dialect semantics, and reject unsupported dialects/transforms instead of accepting arbitrary values.

- [ ] **Step 4: Build collision-safe grouped input schemas**

Emit only used `path`, `query`, and `body` groups, require a group when it contains required input, set `additionalProperties: false` only on gateway-created wrappers, preserve body semantics, and exclude auth headers, destination hosts, and HTTP methods from model input.

- [ ] **Step 5: Compile immutable path/query serializers**

Represent paths as literal/parameter segments. Reject dot segments, slash/backslash, and encoded equivalents; encode each accepted value as one segment. Preserve endpoint base paths and distinguish omitted, false, zero, empty string, and explicit null. Emit repeated keys for exploded arrays.

- [ ] **Step 6: Run REST compiler tests**

Run:

```bash
go test ./internal/adapters/rest -run 'Compile|Schema|Parameter|Security|Path|Query' -count=1
```

Expected: REST-02, REST-04, REST-05, REST-06, REST-07, and REST-12 compile/serialization behaviors pass.

- [ ] **Step 7: Commit**

```bash
git add internal/adapters/rest testdata/openapi
git commit -m "feat: compile safe OpenAPI operations into REST plans"
```

### Task 7: Execute REST Plans and Normalize Results

**Files:**
- Create: `internal/adapters/rest/{adapter,request,response,envelope}.go`
- Create: `internal/testkit/recording_http.go`
- Test: `internal/adapters/rest/adapter_test.go`, `tests/integration/rest_adapter_test.go`

**Interfaces:**
- Consumes: `*rest.RESTPlan`, `contracts.Call`, credential binding, safe HTTP transport, endpoint pool, event sink.
- Produces: `rest.Adapter.CallTool(context.Context, contracts.Call, contracts.EventSink) (contracts.Outcome, error)` with gateway-owned REST envelopes.

- [ ] **Step 1: Write the recording-upstream vertical-slice test for REST-01**

Invoke `users.get_user` with `{"path":{"user_id":"u_123"}}`; assert `GET /api/v1/users/u_123`, exactly one configured bearer header, no downstream auth/cookie/forwarding headers, and a complete result containing matching structured and compact-text envelopes.

- [ ] **Step 2: Implement fresh request construction**

Validate arguments with the compiled schema, build URL/path/query/body without normalization surprises, attach the end-to-end context, create a new header map, inject only the credential broker's headers and allowlisted trace metadata, and close every response body.

- [ ] **Step 3: Test and implement result normalization**

Map JSON, bounded plain text, 204, and 202 into `{ok,status,data}`; preserve 202 as accepted rather than completed work. Validate response schema before producing success. Emit an output schema only when every possible normalized envelope matches it.

- [ ] **Step 4: Test and implement safe failure categories**

Map 404/409/422 to completed `isError` business results, upstream 401 to `UPSTREAM_AUTH_FAILED` without a downstream OAuth challenge, malformed/schema-invalid data to `UPSTREAM_PROTOCOL_ERROR`, and ambiguous post-dispatch failures to `UPSTREAM_OUTCOME_UNKNOWN` with execution ID and `retryable:false`.

- [ ] **Step 5: Enforce decompressed, parsed, and encoded size bounds**

Use limit-plus-one for compressed/decompressed reads, include structured plus text copies in the output limit, reject unsupported binary media, return `RESULT_TOO_LARGE` without truncation, and retain outcome certainty if the action already completed.

- [ ] **Step 6: Run REST execution tests**

Run:

```bash
go test ./internal/adapters/rest ./tests/integration -run 'REST|Envelope|Business|Oversize|SchemaViolation' -count=1
```

Expected: REST-01, REST-03, REST-08, REST-09, REST-10, and REST-11 pass.

- [ ] **Step 7: Commit**

```bash
git add internal/adapters/rest internal/testkit/recording_http.go tests/integration/rest_adapter_test.go
git commit -m "feat: execute REST tools with bounded result translation"
```

### Task 8: Ordered Execution, Admission, Retries, Cancellation, and Audit

**Files:**
- Create: `internal/executor/{executor,state,admission,buffers,retry,correlation}.go`
- Create: `internal/telemetry/{events,redact}.go`
- Create: `internal/testkit/{clock,event_sink}.go`
- Test: `internal/executor/*_test.go`, `internal/telemetry/events_test.go`

**Interfaces:**
- Consumes: snapshot store, router, authorizer, validators, credential broker, adapter registry, endpoint pools, telemetry sink.
- Produces: `executor.Execute(context.Context, Request, contracts.EventSink) (contracts.Outcome, error)` and cancellation lookup scoped by principal/session/request context.

- [ ] **Step 1: Write a table-driven state-machine test**

Assert valid transitions `RECEIVED -> AUTHORIZED -> QUEUED -> DISPATCHING -> RUNNING -> terminal`; inject failure/cancellation at each boundary and assert every acquired snapshot, queue slot, buffer permit, adapter lease, and endpoint inflight token is released exactly once.

- [ ] **Step 2: Implement admission in the specified order**

Acquire global request budget, per-principal token bucket, route/upstream inflight slot, conservative buffer reservation, then endpoint connection. Cap queued requests at 64, bound limiter/session key maps with idle eviction, and charge all waits to the same end-to-end deadline.

- [ ] **Step 3: Implement dispatch recheck and audit linearization**

After credentials/adapter acquisition, call `RecheckAtDispatch` against current policy/quarantine/credential epochs. Record dispatch immediately before the native request with execution ID, trace ID, server/tool/native/upstream/endpoint, pseudonymous principal partition, route revision, policy epoch, attempt, and timing—never raw payloads.

- [ ] **Step 4: Implement side-effect-safe retries**

Keep `max_attempts=1` unless both route behavior is `read_only` and a configured internal failure/status is retryable. Use full-jitter exponential delay bounded by remaining time; recheck revocation for each attempt. Do not add replay-enabling request markers to writes and never retry auth, validation, business `isError`, or protocol incompatibility.

- [ ] **Step 5: Implement correlation and scoped cancellation**

Track HTTP, downstream JSON-RPC, execution, upstream request, and trace IDs separately. Index cancellation by authenticated principal plus modern request context or legacy session/request tuple; record `OUTCOME_UNKNOWN` whenever dispatch occurred without a definitive response.

- [ ] **Step 6: Run executor reliability tests**

Run:

```bash
go test -race ./internal/executor ./internal/telemetry -run 'State|Release|Admission|Buffer|Retry|Cancel|Audit' -count=1
```

Expected: AUTH-04, AUTH-09, REL-03, REL-04, REL-10, and REL-13 behavior.

- [ ] **Step 7: Commit**

```bash
git add internal/executor internal/telemetry internal/testkit
git commit -m "feat: add bounded side-effect-safe execution lifecycle"
```

### Task 9: HTTP Edge and Modern MCP 2026-07-28 Facade

**Files:**
- Create: `internal/edge/{server,limits,authority}.go`
- Create: `internal/protocol/modern/{facade,metadata,methods,sse,errors}.go`
- Test: `internal/edge/server_test.go`, `internal/protocol/modern/*_test.go`, `tests/integration/modern_rest_test.go`

**Interfaces:**
- Consumes: exact virtual-server router, identity verifier, catalog service, executor, MCP SDK primitives.
- Produces: `modern.Handler(Dependencies) http.Handler` supporting `server/discover`, `tools/list`, `tools/call`, cancellation, and request-scoped SSE only.

- [ ] **Step 1: Write failing edge tests**

Reject invalid host/origin, disallowed missing origin, oversized headers/body, batches, malformed JSON, wrong path/trailing slash, and duplicate JSON object members (`method`, tool `name`, or `_meta`) before identity/schema/upstream work. Assert zero upstream calls.

- [ ] **Step 2: Implement pre-SDK edge validation**

Apply header/body/depth/parsing-time limits; exact host/path lookup; origin policy; duplicate-member detection with a streaming token pass; and authentication before handing a bounded body to the modern facade.

- [ ] **Step 3: Write and implement metadata/header validation**

Require version, client info, and capabilities per request; compare every required HTTP mirror with the parsed body; reject missing/mismatched values as version-correct protocol errors. Rebuild metadata independently for any upstream call.

- [ ] **Step 4: Implement modern methods and errors**

Serve discovery and authorized paginated tool lists locally; execute tools through `executor.Execute`; preserve complete/protocol-error distinctions and structured native content. Return HTTP 404 plus JSON-RPC method-not-found for unknown methods and reject all unadvertised capability families.

- [ ] **Step 5: Implement bounded request-scoped SSE progress**

Flush immediately, cap queued event count/bytes, correlate progress to the request, cancel upstream best-effort when the modern response stream closes, and end with one final result/error. Use separate parse and execution timeouts.

- [ ] **Step 6: Add the started-stream auth/policy regression test**

Start SSE, then revoke the route before dispatch/finalization. Assert the handler emits a permitted terminal protocol/result error or closes safely; assert it never attempts to write a second HTTP status and never dispatches after revocation.

- [ ] **Step 7: Prove the modern MCP-to-REST vertical slice**

Run:

```bash
go test ./internal/edge ./internal/protocol/modern ./tests/integration -run 'Modern|DuplicateMember|SSE|RESTVerticalSlice' -count=1
```

Expected: MCP-01, MCP-02, MCP-10, MCP-11, MCP-12, REL-05, and Review Focus items 1/4 pass.

- [ ] **Step 8: Commit**

```bash
git add internal/edge internal/protocol/modern tests/integration/modern_rest_test.go
git commit -m "feat: expose modern MCP over a hardened HTTP edge"
```

### Task 10: Remote MCP Discovery, Invocation, and Contract Refresh

**Files:**
- Create: `internal/adapters/mcp/{adapter,discover,http_client,translate,refresh}.go`
- Create: `testdata/protocol/{modern-discover,legacy-initialize,tool-content}.json`
- Test: `internal/adapters/mcp/*_test.go`, `tests/integration/remote_mcp_test.go`

**Interfaces:**
- Consumes: MCP SDK v1.8.0, credential-partitioned client/session factory, endpoint pool, expected native fingerprints.
- Produces: `mcp.Adapter` implementing `contracts.ToolAdapter`, `DiscoveryResult{Version,Catalog,Fingerprint}`, and refresh events for the quarantine overlay.

- [ ] **Step 1: Write safe protocol-detection tests**

Assert `server/discover` is attempted first under a short deadline; fallback to legacy initialization occurs only for documented version evidence; auth/header/ambiguous server errors do not trigger mutating probes; and no tool executes during detection.

- [ ] **Step 2: Implement bounded discovery**

For modern, consume discover then all tool pages. For legacy, complete initialize plus `notifications/initialized` before listing. Cap at 100 pages, 10,000 tools, configured bytes/duration; reject repeated cursors, duplicate native names, invalid schemas, and unsupported required features.

- [ ] **Step 3: Implement public/native translation**

Keep public and native names separate, issue a distinct upstream request ID, rebuild version metadata and mirrors including `Mcp-Name`, preserve validated business arguments, and never copy downstream identity into upstream `clientInfo`.

- [ ] **Step 4: Preserve native outcomes**

Round-trip text/image/audio/resource content blocks, structured content, `isError`, and negotiated metadata. Sanitize and re-ID valid upstream JSON-RPC errors; map malformed messages to `UPSTREAM_PROTOCOL_ERROR`; reject interactive input requirements explicitly in v1. Treat tool text/results that request configuration, credentials, commands, or scopes as untrusted data and never as control-plane actions (AUTH-07).

- [ ] **Step 5: Implement refresh/quarantine rules**

Poll with injected-clock jitter. Refresh unchanged validity; leave additions unpublished; immediately quarantine changed/removed exported contracts; retain network-failed contracts only through `max_stale`, then quarantine. Never mutate a leased route.

- [ ] **Step 6: Run remote MCP tests**

Run:

```bash
go test ./internal/adapters/mcp ./tests/integration -run 'Detect|Discover|Alias|Content|Refresh|Quarantine' -count=1
```

Expected: MCP-04, MCP-07 through MCP-09, MCP-11, MCP-13, REL-11, and REL-12 pass.

- [ ] **Step 7: Commit**

```bash
git add internal/adapters/mcp testdata/protocol tests/integration/remote_mcp_test.go
git commit -m "feat: add isolated remote MCP discovery and invocation"
```

### Task 11: Legacy MCP 2025-11-25 Facade and Cross-Era Translation

**Files:**
- Create: `internal/protocol/legacy/{facade,lifecycle,sessions,translate,cancel}.go`
- Test: `internal/protocol/legacy/*_test.go`, `tests/conformance/cross_era_test.go`

**Interfaces:**
- Consumes: common catalog/executor, remote MCP adapter, authenticated principal, injected clock.
- Produces: gateway-owned bounded legacy sessions and compatible legacy tool catalogs/results.

- [ ] **Step 1: Write lifecycle/session tests**

Require `initialize` then `notifications/initialized`; bind session to server, issuer/subject/tenant/client partition, protocol, and capabilities; authenticate every request; reject cross-principal use independently of whether a session exists; expire after 15 minutes and cap at 1,000.

- [ ] **Step 2: Implement the isolated legacy facade**

Support initialize, initialized notification, legacy ping, tools list/call, explicit cancellation, and optional session header. Return 405 for standalone GET SSE/replay. Never treat legacy initialization as malformed modern metadata.

- [ ] **Step 3: Implement protocol-compatible catalog filtering**

Translate ordinary legacy results to modern complete results. For legacy clients, omit modern tools whose structured output is array/scalar or otherwise cannot satisfy the legacy result contract; do not wrap native MCP output or change its schema.

- [ ] **Step 4: Implement disconnect and cancellation semantics**

Detach legacy execution from POST response lifetime, retain snapshot/adapter resources until execution ends, keep the hard deadline, and cancel only after an authenticated session-scoped cancellation notification. Pin implicit legacy upstream state to its endpoint/session.

- [ ] **Step 5: Run the cross-era matrix**

Run:

```bash
go test -race ./internal/protocol/legacy ./tests/conformance -run 'Legacy|CrossEra|Session|Disconnect|Cancel' -count=1
```

Expected: MCP-03, MCP-04, MCP-05, MCP-06, AUTH-04, AUTH-11, and REL-06 pass.

- [ ] **Step 6: Commit**

```bash
git add internal/protocol/legacy tests/conformance/cross_era_test.go
git commit -m "feat: add legacy MCP lifecycle and cross-era compatibility"
```

### Task 12: Protocol Conformance and Facade Isolation Gate

**Files:**
- Create: `tests/conformance/{modern_http,legacy_http,errors,metadata}.go`
- Create: `docs/adr/0002-protocol-facades.md`
- Modify: `Makefile`, `.github/workflows/ci.yml`

**Interfaces:**
- Consumes: modern/legacy handlers and the official conformance runner fixtures.
- Produces: repeatable `make conformance` covering every enabled era/transport direction and an ADR documenting SDK wrapping decisions.

- [ ] **Step 1: Build the table-driven conformance harness**

Enumerate downstream modern/legacy × upstream REST/modern MCP/legacy MCP, with expected discovery lifecycle, metadata, result shape, cancellation signal, session behavior, and unsupported capability response.

- [ ] **Step 2: Add golden wire tests**

Compare status, headers, JSON-RPC code/shape, SSE framing, and cache fields against checked-in canonical fixtures. Normalize only nondeterministic execution/request/trace IDs; never golden-ignore version, name, result type, or error category.

- [ ] **Step 3: Add CI commands**

Run:

```bash
go test ./tests/conformance -count=1
go test -race ./internal/protocol/... ./internal/adapters/mcp/... -count=1
```

Expected: every matrix cell passes and SDK types remain confined to protocol/MCP-adapter packages.

- [ ] **Step 4: Commit**

```bash
git add tests/conformance docs/adr/0002-protocol-facades.md Makefile .github/workflows/ci.yml
git commit -m "test: gate modern and legacy protocol conformance"
```

### Task 13: Supervised Stdio MCP Upstreams

**Files:**
- Create: `internal/supervisor/{supervisor,process,framing,partition,backoff}.go`
- Create: `internal/adapters/mcp/stdio_client.go`
- Create: `testdata/stdio/helper/main.go`
- Test: `internal/supervisor/*_test.go`, `tests/faults/stdio_test.go`

**Interfaces:**
- Consumes: fixed absolute executable/args/cwd/env configuration, isolation key, MCP adapter contracts.
- Produces: process leases keyed by generation+tenant+subject/client+credential epoch+session when needed, with `STOPPED/STARTING/DISCOVERING/READY/FAILED/BACKOFF/DRAINING/CRASHED` transitions.

- [ ] **Step 1: Write launch-boundary tests**

Assert absolute operator-selected executable, fixed argv, allowlisted environment, fixed cwd, no shell, no argument substitution from tool input, bounded process count/startup attempts, and distinct per-principal/credential partitions.

- [ ] **Step 2: Implement one reader and serialized writer per process**

Use newline-framed JSON-RPC with an explicit frame limit, continuous bounded stderr draining/redaction, a single stdout protocol reader, and serialized stdin writes. Treat malformed/oversized stdout as process protocol failure rather than tool text.

- [ ] **Step 3: Implement supervisor state/backoff**

Discover before READY, default to one inflight call, use exponential restart backoff with full jitter and ceiling, never replay unfinished calls after crash, and lazily create execution partitions that verify the expected contract.

- [ ] **Step 4: Add the shared-child cancellation regression test**

Qualify the helper for two concurrent calls; make one time out and assert the other completes without process termination. Then use a single-inflight hung helper and assert only its partition is terminated after supervisor grace with unknown outcome where dispatch occurred.

- [ ] **Step 5: Implement bounded drain and exact reaping**

Stop new leases, close stdin, wait the grace interval, signal the process group, kill only after the limit, close pipes, and reap exactly once. Ensure stderr flood and huge frames cannot block shutdown.

- [ ] **Step 6: Run stdio fault tests**

Run:

```bash
go test -race ./internal/supervisor ./tests/faults -run 'Stdio|Frame|Stderr|Shared|Drain|Reap' -count=1
```

Expected: AUTH-08, REL-07, REL-08, and Review Focus item 5 pass.

- [ ] **Step 7: Commit**

```bash
git add internal/supervisor internal/adapters/mcp/stdio_client.go testdata/stdio tests/faults/stdio_test.go
git commit -m "feat: supervise isolated stdio MCP upstreams"
```

### Task 14: Startup, Reload, Revocation, and Graceful Shutdown

**Files:**
- Create: `internal/runtime/{startup,reload,shutdown}.go`
- Create: `internal/app/serve.go`
- Test: `internal/runtime/{startup,reload,shutdown}_test.go`, `tests/faults/lifecycle_test.go`

**Interfaces:**
- Consumes: compiler, store, listeners, discovery manager, supervisor, telemetry sink, OS signals.
- Produces: serialized startup/reload/shutdown coordinator and readiness state with reasons.

- [ ] **Step 1: Write startup readiness tests**

Start admin diagnostics not-ready; require valid config, credentials, all required contracts, candidate compilation, data-listener bind, and activation before ready. Assert optional unavailable upstreams are dormant and visible in diagnostics, while required failure exits nonzero within 30 seconds.

- [ ] **Step 2: Implement transactional SIGHUP reload**

Serialize/coalesce signals; compile and prepare candidate resources off-store; compare accepted manifests/listener topology; return `RESTART_REQUIRED` for listener changes; publish once; discard candidate-only resources on failure; preserve the old generation and working requests.

- [ ] **Step 3: Test reload/session reconciliation**

Exercise reload overlapping lease acquisition, failed online reload, secret rotation, removed servers, changed identity bindings, and implicit-state retirement. Assert old sessions never preserve stale permissions and long calls keep their lease until completion/drain deadline.

- [ ] **Step 4: Implement emergency overlay publication**

Publish principal/export/upstream denies under a small independent lock/atomic snapshot; increment policy epoch; make queued calls recheck at dispatch; cancel already-dispatched work best-effort without claiming rollback.

- [ ] **Step 5: Implement graceful SIGTERM**

Mark not-ready, stop admission, cancel discovery/reload, drain calls for 45 seconds, close sessions/transports, cancel remaining executions with version-correct semantics, reap children, and bound telemetry flush. Exit nonzero only for documented critical internal failures.

- [ ] **Step 6: Run lifecycle fault tests**

Run:

```bash
go test -race ./internal/runtime ./tests/faults -run 'Startup|Reload|Revocation|Shutdown|Lease' -count=1
```

Expected: CFG-05, CFG-06, CFG-09, REL-01, REL-02, REL-09, REL-11, REL-12, and REL-14 pass.

- [ ] **Step 7: Commit**

```bash
git add internal/runtime internal/app/serve.go tests/faults/lifecycle_test.go
git commit -m "feat: add transactional lifecycle and graceful drain"
```

### Task 15: CLI, Administration, Metrics, and Inspection

**Files:**
- Create: `internal/app/{validate,inspect,version}.go`
- Create: `internal/admin/{server,status}.go`
- Create: `internal/telemetry/{metrics,tracing}.go`
- Test: `internal/app/*_test.go`, `internal/admin/*_test.go`, `internal/telemetry/*_test.go`

**Interfaces:**
- Consumes: compiler online/offline modes, runtime status snapshots, Prometheus registry, OpenTelemetry tracer provider.
- Produces: exact CLI contract and loopback admin endpoints `/livez`, `/readyz`, `/v1/status`, `/v1/upstreams`, `/v1/routes`, `/metrics`.

- [ ] **Step 1: Implement and test offline/online validation**

`validate --config` performs structural/semantic/local OpenAPI checks and reports unresolved online contracts without claiming readiness. `--online` additionally performs safe discovery/credential checks. Both emit machine-readable diagnostics and never expose secret values.

- [ ] **Step 2: Implement and test redacted inspection/version output**

`inspect --format json` lists exports, methods, policy references, fingerprints, protocol compatibility, dormant/quarantined states, and diagnostics. `version --json` reports executable/commit, Go 1.27.3, MCP SDK v1.8.0, configuration `mcpgate/v1`, and tested protocol versions.

- [ ] **Step 3: Implement separated admin handlers**

Bind only the configured admin listener (requiring loopback in v1). Keep all admin paths off the public MCP mux. Make liveness local-only, readiness reflect active generation/required dependencies, and health/status reads non-mutating.

- [ ] **Step 4: Implement bounded telemetry**

Register the metric names from Section 21 with only server/tool/upstream/method/outcome/reason/state labels. Reject or bucket configured label cardinality above catalog limits. Create spans for validation, queueing, credentials, upstream execution, and normalization; forward only allowlisted trace context and no arbitrary baggage.

- [ ] **Step 5: Test redaction across every output surface**

Seed tokens, session IDs, raw arguments/results, child stderr, cursor material, and low-entropy user IDs; assert none appear in CLI JSON, admin JSON, metrics text, structured logs, traces, panic recovery, or SDK logger output. Assert the keyed principal digest remains stable only under the configured audit key.

- [ ] **Step 6: Run operations tests**

Run:

```bash
go test ./internal/app ./internal/admin ./internal/telemetry -run 'Validate|Inspect|Version|Health|Metrics|Redact' -count=1
```

Expected: CLI contracts, admin separation, bounded labels, and diagnostic redaction pass.

- [ ] **Step 7: Commit**

```bash
git add internal/app internal/admin internal/telemetry
git commit -m "feat: add safe CLI administration and telemetry"
```

### Task 16: Release Hardening, Packaging, and Qualification Evidence

**Files:**
- Create: `tests/integration/gateway_test.go`
- Create: `tests/conformance/release_matrix_test.go`
- Create: `tests/faults/{network,memory,shutdown}_test.go`
- Create: `tests/benchmarks/gateway_test.go`
- Create: `internal/config/fuzz_test.go`, `internal/catalog/fuzz_test.go`, `internal/adapters/rest/fuzz_test.go`, `internal/protocol/modern/fuzz_test.go`
- Create: `Dockerfile`, `.dockerignore`, `docs/{operations,security,release-checklist}.md`
- Modify: `Makefile`, `.github/workflows/ci.yml`, `README.md`

**Interfaces:**
- Consumes: the complete gateway and all Section 23 IDs.
- Produces: a reproducible static Linux binary/container plus a release report mapping CFG-01..10, REST-01..12, MCP-01..14, AUTH-01..11, and REL-01..14 to passing tests.

- [ ] **Step 1: Create the acceptance manifest and fail on missing IDs**

Define `tests/release-cases.json` with every required ID, owning test package, exact test name, and evidence command. Add a test that compares the manifest to the specification's complete ID sets and fails on omissions or duplicates.

- [ ] **Step 2: Complete hostile parser/serializer fuzz targets**

Fuzz YAML AST validation, JSON duplicate-member detection, cursor decode/signature checks, OpenAPI path/query serialization, protocol metadata/header mirrors, and JSON number preservation. Each target must enforce bounded time/memory and assert no panic, credential leak, path escape, or accepted ambiguity.

- [ ] **Step 3: Complete deterministic fault tests**

Use injected clocks/dialers/upstreams for connection drop after write, slow consumer, compressed bomb, DNS change, repeated discovery cursor, incompatible replicas, response schema violation, refresh staleness, blocked log sink, concurrent near-limit responses, and shutdown under traffic.

- [ ] **Step 4: Build the hardened container**

Use a Go 1.27.3 build stage and distroless/static nonroot runtime, read-only-compatible paths, no shell/package manager, no baked secrets, explicit healthcheck to admin readiness, and no published admin port. Document a separate stdio image profile that copies only approved child executables and dependencies.

- [ ] **Step 5: Write deployment/security documentation**

Document TLS ingress requirement, service-identity limitations, object-authorization boundary, in-memory session/rate-limit loss on restart, per-instance quotas, legacy affinity, no exactly-once guarantee, unknown-outcome reconciliation, stdio non-sandbox warning, admin restriction, secret rotation, and replica configuration/cursor-key consistency.

- [ ] **Step 6: Run the full release gate**

Run:

```bash
go test ./... -count=1
go test -race ./... -count=1
go test ./tests/conformance -count=1
go test ./tests/faults -count=1
go vet ./...
govulncheck ./...
go test ./tests/benchmarks -run '^$' -bench BenchmarkGateway -benchtime=30m
docker build --pull=false -t mcpgate:local .
```

Expected: all named acceptance IDs pass; no race/vulnerability finding remains untriaged; the benchmark records machine/build/payload/catalog/upstream parameters, p95 gateway-added processing below 5 ms for 1,000 tools/100 concurrent 1 KiB calls/20 ms mock latency, stable goroutine/FD counts, and memory within configured budgets.

- [ ] **Step 7: Record reproducible release evidence**

Generate `artifacts/release-report.json` containing commit, dependency hashes, Go version, test/race/fuzz/conformance results, image digest, benchmark environment/results, and any formally accepted exception. Do not commit transient binaries or secret-bearing logs.

- [ ] **Step 8: Commit**

```bash
git add tests internal Dockerfile .dockerignore docs Makefile .github/workflows/ci.yml README.md
git commit -m "release: qualify universal mcpgate v1"
```

## Final Verification Checklist

- [ ] `rg -n 'TBD|TODO|implement later|@latest' --glob '!docs/superpowers/plans/**' .` returns no release source/config placeholders or unpinned dependency commands.
- [ ] `go list -m all` matches the reviewed lock set and `govulncheck ./...` has no unexplained finding.
- [ ] `go test -race ./... -count=1` passes, including reload/acquire, session/cancellation, credential rotation, and stdio ownership cases.
- [ ] Every required acceptance ID occurs exactly once in `tests/release-cases.json` and points to a passing test.
- [ ] Modern and legacy golden wire fixtures prove different lifecycle/session/cancellation behavior rather than one permissive shared handler.
- [ ] A repository-wide import check confirms MCP SDK types appear only in `internal/protocol/**` and `internal/adapters/mcp/**`.
- [ ] A repository-wide redaction test confirms seeded secrets/payloads/session IDs do not appear in revisions, diagnostics, CLI/admin output, logs, metrics, traces, SDK logs, or child stderr.
- [ ] The container runs as nonroot with a read-only root filesystem, accepts read-only config/secret mounts, and does not expose the admin listener publicly.
- [ ] The release report states the non-durable boundaries: sessions, counters, discovery freshness, audit stdout, and exactly-once execution are not persisted.
