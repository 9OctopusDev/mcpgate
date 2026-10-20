# Universal MCP Gateway

## Technical Implementation Specification

**Working executable name:** `mcpgate`  
**Document version:** 1.0  
**Prepared:** October 19, 2026  
**Status:** Proposed implementation contract, not an implemented product  
**Target:** Standalone Go binary and container; Linux first

This document turns the preceding gateway architecture into an engineering specification. It defines implementation boundaries, ownership, configuration, protocol behavior, execution algorithms, failure semantics, and release tests. The accompanying configuration, schema, fixtures, and Go interfaces are reference assets for the application to be built.

The words **must**, **should**, and **may** express project requirements unless the paragraph explicitly cites the MCP or OpenAPI specification. Source identifiers such as [S03] refer to the source register at the end. Design choices are not claims that those choices are required by MCP.

## 1. Product contract and architecture decisions

**Build a configuration-driven, application-layer gateway, not middleware installed inside the upstream application and not an NGINX module.** The process terminates downstream MCP, authorizes a capability call, and originates an independent REST or MCP request upstream. Backend applications require no code changes.

The deployment promise is **one reusable gateway instead of a separately developed MCP service for each application**. A gateway process still has to run. “No additional infrastructure” means no mandatory database, Redis, message broker, hosted control plane, or model provider for the standalone feature set; it does not mean zero deployment or operational work.

| Decision | Implementation consequence |
| --- | --- |
| One executable, two logical planes | Configuration/discovery and request execution run in the same process with separate packages and resource budgets. |
| Go data plane | Use the standard HTTP stack and a pinned official MCP SDK behind gateway-owned interfaces. |
| Explicit capability exports | No automatic publication of newly discovered operations or wildcard export in v1. |
| Protocol termination on both sides | Do not forward raw downstream JSON-RPC identifiers, sessions, credentials, or transport headers blindly. |
| Immutable route generations | Compile a complete candidate and publish it as one transaction. |
| Tools-first v1 | REST tools and ordinary tools from HTTP/stdio MCP upstreams are required; later capability families are explicit extensions. |
| No semantic routing | Public tool names resolve deterministically. No LLM, embedding index, or task classifier is in the execution path. |
| Safe retry default | One explicit execution attempt; writes are not automatically replayed. |

The gateway does not replace business authorization inside an API. A static upstream service credential intentionally exposes that service identity’s powers to the callers allowed by the gateway policy. It cannot magically preserve user-level object permissions that the API has never been configured to enforce.

## 2. Release scope and verified protocol baseline

### 2.1 Version decisions

The official specification’s latest link resolved to **MCP 2026-07-28** when checked for this document. Use that revision as the primary protocol contract and **2025-11-25** as the explicitly tested legacy contract. Do not infer compatibility with every version supported by a dependency. [S01, S02]

These revisions cannot share a single assumed lifecycle. Modern requests carry their version and capabilities per request; legacy connections begin with initialization. Isolate those behaviors in distinct protocol facades, with upstream and downstream versions selected independently. [S02, S15]

The official Go SDK compatibility table identifies v1.7.0 and later as supporting the modern revision. Select and lock a reviewed exact SDK release at implementation kickoff; the compatibility table is not a substitute for gateway integration tests. The proposed production toolchain baseline is Go 1.27.2, listed in the official release history on October 8, 2026. [S17, S22]

### 2.2 Feature boundary

| Capability | v1.0 requirement | Subsequent extension |
| --- | --- | --- |
| Downstream MCP | Modern Streamable HTTP plus legacy Streamable HTTP compatibility | Additional versions after conformance tests |
| REST upstream | OpenAPI 3.0.x/3.1.x, supported parameter subset, JSON requests/responses | Multipart, binary downloads, manual mappings, additional OpenAPI versions |
| MCP upstream | Modern/legacy Streamable HTTP and supervised stdio | Legacy HTTP+SSE transport only with a separate adapter |
| MCP tools | Explicit discovery, list pagination, calls, native content, progress, cancellation | Interactive bridging and subscriptions |
| Resources and prompts | Not advertised; unsupported methods rejected | Separate typed adapters and URI mapping |
| Authentication | Existing OAuth resource-server integration; configured upstream credentials | Delegated OAuth/token exchange, consent and durable grants |
| Management | File configuration, validation, inspection, signal-driven reload | Remote control plane and administrative UI |
| State | In-process leases, catalogs, counters and legacy sessions | Optional persistence when durable workflows require it |

“Universal” describes the supported **upstream categories**, not transparent support for every optional MCP feature. v1 must not advertise elicitation, sampling, roots, tasks, resources, prompts, or list-change delivery that it cannot faithfully implement. A tool that depends on those features is not v1-compatible.

### 2.3 Protocol facade matrix

| Concern | Modern facade: 2026-07-28 | Legacy facade: 2025-11-25 |
| --- | --- | --- |
| Entry lifecycle | Implement `server/discover`; initialization is not a prerequisite | `initialize`, then `notifications/initialized` |
| HTTP communication | POST with JSON or request-scoped SSE | POST plus optional GET SSE behavior |
| Sessions | No MCP protocol-session identifier | Gateway-owned session state when needed |
| Cancellation | Response-stream closure cancels that modern request | Explicit cancellation; disconnect alone is not cancellation |
| Normal result | Modern result fields, including `resultType` | Encode the legacy result shape |
| Discovery caching | Modern cache hints | Do not add modern-only required fields |

The transport differences and legacy disconnect rule are specified by the two transport revisions. SDK behavior must be checked against both; an HTTP handler option called “stateless” does not by itself establish protocol compatibility. [S03, S14]

## 3. Process architecture and module ownership

```text
                       mcpgate process
+-----------------------------------------------------------+
| Control plane                                             |
| Config Loader -> Compiler -> Candidate Runtime -> Publisher|
|      OpenAPI Loader | MCP Discovery | Policy Compiler      |
+-------------------------------+---------------------------+
                                | immutable generation
+-------------------------------v---------------------------+
| Data plane                                                |
| HTTP Edge -> Protocol Facade -> Auth -> Router -> Executor |
|                                            |              |
|                         +------------------+-----------+  |
|                         |                  |           |  |
|                    REST Adapter       MCP HTTP     MCP IO |
|                         |                  |           |  |
|                   Existing API       Remote MCP    stdio  |
+-----------------------------------------------------------+
| Shared runtime services                                   |
| Credentials | Admission | Pools | Session Store | Telemetry|
+-----------------------------------------------------------+
```

| Package | Owns | Must not own |
| --- | --- | --- |
| `config` | Strict decoding, defaults, source paths, structural validation | Live request processing |
| `compiler` | OpenAPI normalization, exports, schema compilation, diagnostics | In-place mutation of active routes |
| `runtime` | Snapshot leases, atomic publication, resource retirement | Tool business logic |
| `edge` | Listener limits, host/origin checks, HTTP framing | Upstream authorization decisions |
| `protocol` | Modern/legacy codecs, version selection, wire errors | URL assembly or secrets lookup |
| `catalog` | Authorized capability views, fingerprints, cursors | Caller-controlled publication |
| `router` | Exact virtual-server/tool lookup | Upstream discovery on ordinary calls |
| `policy` | Tenant/subject/scope decisions and dispatch rechecks | Trusting tool descriptions as policy |
| `credentials` | Secret resolution, identity binding, credential epochs | Returning tokens in tool output |
| `executor` | Deadlines, admission, attempts, result/error classification | Transport-specific handshakes |
| `upstream` | HTTP pools, MCP sessions, process supervisors and health | Shared mutable default authorization headers |
| `adapters/rest` | REST request plans and response normalization | Executing arbitrary supplied URLs |
| `adapters/mcp` | Native discovery, name rewriting and MCP calls | Changing native business semantics |
| `admin` | Local status, diagnostics, reload orchestration | Public tool execution |
| `telemetry` | Redacted events, metrics, traces | Unbounded payload retention |

A failure in discovery must not monopolize the call executor. Give the control plane its own concurrency cap, context tree, and memory limits. A process supervisor belongs to the upstream manager, not to an individual tool-call goroutine.

## 4. Repository layout and dependency policy

```text
mcpgate/
  cmd/mcpgate/main.go
  internal/
    config/          compiler/        runtime/
    edge/            protocol/modern/ protocol/legacy/
    catalog/         router/          policy/
    credentials/     executor/        upstream/
    adapters/rest/   adapters/mcp/    supervisor/
    admin/           telemetry/
  schemas/gateway-config.v1.schema.json
  examples/gateway.yaml
  testdata/openapi/
  testdata/protocol/
  tests/integration/
  tests/conformance/
  tests/faults/
  tests/benchmarks/
  docs/adr/
  go.mod
  go.sum
```

Use `net/http` for listeners and HTTP pools; the official MCP Go SDK for MCP codecs/client-server primitives; `libopenapi` for document parsing; and a validator with JSON Schema 2020-12 support, such as `santhosh-tekuri/jsonschema`. Those projects document the relevant capabilities, but parser support for a format does not imply that this gateway implements every operation it can parse. [S17, S19, S20, S21]

Keep all SDK types inside `internal/protocol` and `internal/adapters/mcp`. The rest of the application uses gateway-owned contracts. This prevents an SDK upgrade from changing policy or runtime ownership APIs.

Commit exact dependency versions and `go.sum`. Use a supported, patched Go toolchain, dependency vulnerability scanning, and the protocol conformance matrix as release gates. Do not use `@latest` in release builds. v1 has compiled-in adapters only: no arbitrary in-process plugins or runtime package installation.

## 5. Configuration contract

### 5.1 Structural rules

`gateway.example.yaml` is a full example of the proposed v1 language. The accompanying `gateway-config.v1.schema.json` is its strict structural schema. **These files describe the application to implement; they are not configuration for an existing gateway product.**

Require one UTF-8 YAML document. Reject duplicate mapping keys, unknown fields, custom YAML tags, and excessive nesting or aliases. Do not perform shell expansion or general `${...}` substitution. Secrets are resolved only through named `env` or `file` providers.

Resolve ordinary relative files against the configuration directory, never the process working directory. Restrict file references to configured source roots, reject symlink escapes, and apply byte limits before parsing. In v1, remote OpenAPI documents and external schema references are disabled.

`servers` are virtual MCP servers selected by exact request path after host validation. Each export has an explicit **public name**, **upstream**, **native operation**, and **policy**. The public name is the exact identifier returned by discovery and accepted by invocation. No name normalization happens during a call.

### 5.2 Complete example

```yaml
# Proposed mcpgate v1 configuration; example hosts are placeholders.
api_version: mcpgate/v1

gateway:
  name: universal-mcp
  data:
    listen: "127.0.0.1:8080"
    allowed_hosts: ["mcp.example.com", "localhost:8080"]
    allowed_origins: ["https://console.example.com"]
    allow_missing_origin: true
  admin:
    listen: "127.0.0.1:9090"
  limits:
    max_inflight: 256
    max_queued: 64
    queue_timeout: 250ms
    call_timeout: 30s
    request_bytes: 1048576
    response_bytes: 4194304
    schema_bytes: 2097152
    buffer_budget_bytes: 268435456
    per_principal_rps: 20
    per_principal_burst: 40
  lifecycle:
    startup_timeout: 30s
    drain_timeout: 45s
    legacy_session_idle: 15m
    max_legacy_sessions: 1000

secrets:
  users_token: {source: env, name: USERS_API_TOKEN}
  issues_token: {source: file, path: /run/secrets/issues-token}
  cursor_key: {source: env, name: MCP_CURSOR_SIGNING_KEY}

authentication:
  corp:
    kind: oauth_resource_server
    issuer: "https://identity.example.com/"
    jwks_uri: "https://identity.example.com/.well-known/jwks.json"
    algorithms: [RS256]
    tenant_claim: tenant_id
    scope_claim: scope

policies:
  users_reader:
    tenants: [acme]
    scopes_all: ["users:read"]
  issue_reader:
    tenants: [acme]
    scopes_all: ["issues:read"]
  local_reader:
    tenants: [acme]
    subjects: [engineering-agent]
    scopes_all: ["local:read"]

upstreams:
  users_api:
    kind: rest
    required: true
    endpoints:
      - id: users-a
        url: "https://users.internal.example/api/v1"
        weight: 1
    network:
      allowed_hosts: [users.internal.example]
      allowed_cidrs: ["10.20.0.0/16"]
    auth: {kind: bearer, secret: users_token}
    openapi:
      file: ./openapi/users.openapi.yaml
      external_refs: deny
    execution:
      timeout: 15s
      max_inflight: 32
      max_attempts: 1

  issues_mcp:
    kind: mcp_http
    required: true
    endpoints:
      - id: issues-a
        url: "https://issues-mcp.example.com/mcp"
        weight: 1
    network:
      allowed_hosts: [issues-mcp.example.com]
      allowed_cidrs: public_only
    auth: {kind: bearer, secret: issues_token}
    protocol:
      versions: ["2026-07-28", "2025-11-25"]
      detect: discover_first
    discovery:
      refresh_interval: 60s
      max_stale: 5m
    isolation: per_principal
    state_model: request_scoped
    execution:
      timeout: 20s
      max_inflight: 16
      max_attempts: 1

  local_mcp:
    kind: mcp_stdio
    required: false
    process:
      executable: /opt/mcpgate/bin/local-mcp
      args: ["--root", "/workspace/public-docs"]
      cwd: /workspace/public-docs
      env: {LANG: "C.UTF-8"}
      max_processes: 4
      max_frame_bytes: 4194304
    protocol:
      versions: ["2026-07-28", "2025-11-25"]
      detect: discover_first
    discovery:
      refresh_interval: 60s
      max_stale: 5m
    isolation: per_principal
    state_model: request_scoped
    execution:
      timeout: 10s
      max_inflight: 1
      max_attempts: 1

servers:
  engineering:
    path: /mcp/engineering
    resource_uri: "https://mcp.example.com/mcp/engineering"
    authentication: corp
    protocols: ["2026-07-28", "2025-11-25"]
    features:
      tools: true
      list_changed: false
      interactive_bridge: false
      resources: false
      prompts: false
    catalog:
      page_size: 100
      ttl_ms: 0
      cursor_secret: cursor_key
    exports:
      - name: users.get_user
        upstream: users_api
        native: getUser
        policy: users_reader
        service_identity_acknowledged: true
        behavior: read_only
      - name: issues.search
        upstream: issues_mcp
        native: search_issues
        policy: issue_reader
        service_identity_acknowledged: true
        behavior: read_only
      - name: local.read_file
        upstream: local_mcp
        native: read_file
        policy: local_reader
        service_identity_acknowledged: true
        behavior: read_only

observability:
  logs: json
  payload_logging: false
  metrics: true
  traces: false
```

### 5.3 Field semantics

| Field | Meaning and enforcement |
| --- | --- |
| `api_version` | Version of the gateway configuration language; unrelated to MCP wire versions. |
| `gateway.data` | Development listener and explicit host/origin policy. Production exposes it only through verified TLS termination or a protected ingress. |
| `gateway.admin` | Separate loopback administrative listener. Never add these routes to the public MCP listener. |
| `limits` | Per-process admission, byte and buffer budgets. These are not cluster-wide quotas. |
| `secrets` | References only. A cursor key must decode to at least 32 random bytes. File secrets must be readable only by the intended runtime identity. |
| `authentication` | Trusted issuer, JWKS endpoint, algorithm allowlist and claim mapping. The virtual server’s `resource_uri` is its required token audience. |
| `policies` | All specified conditions are ANDed; values within `tenants` or `subjects` are ORed. Every scope in `scopes_all` is required. |
| `upstreams.*.required` | A required dependency must have a valid discovered contract before candidate activation. Optional unavailable upstreams initially contribute no tools. |
| `endpoints` | Operator-declared replicas of the same logical service. They are not interchangeable unrelated servers. |
| `network` | Host allowlist plus permitted destination address space. `public_only` excludes non-global destinations. |
| `isolation` | `per_principal` partitions logical MCP state; `service_shared` is allowed only for explicitly shared, request-scoped integrations. |
| `state_model` | `request_scoped` means no hidden cross-call conversational state. `implicit_session` requires legacy-session affinity; modern exposure is rejected in v1. |
| `service_identity_acknowledged` | Explicit acceptance that the export acts as the configured upstream service/OS identity, not automatically as the caller. |
| `behavior` | Operator-reviewed execution classification. It constrains retries; it is not inferred from untrusted MCP annotations. |
| `max_attempts` | Upper bound on explicit gateway attempts for eligible read operations. It does not grant permission to replay a write. |
| `features` | v1 schema requires advanced features to remain false. Unsupported flags cannot silently enable partial implementations. |

The example’s hostnames, executable and tokens are placeholders. Its loopback HTTP listener assumes local development or an existing local TLS reverse proxy. A container deployment must intentionally change the listener address and protect the network path; it must not publish the admin port.

The local example’s `--root` argument is only a hypothetical upstream option. It does not create an operating-system sandbox. Apply OS-level isolation independently.

### 5.4 Semantic checks beyond JSON Schema

The compiler must reject missing secret/policy/authenticator/upstream references; duplicate public names or server paths; duplicate endpoint IDs; invalid CIDRs; endpoint hosts outside the allowlist; URL credentials, queries or fragments; non-HTTPS remote endpoints; unsupported selected OpenAPI constructs; and impossible protocol/state combinations.

Reject `max_attempts > 1` for exports whose runtime retry policy cannot establish read-only safety. Reject `implicit_session` exposed to modern clients unless a future explicit-context bridge is configured. Reject any export whose upstream authentication does not satisfy the selected OpenAPI security requirement.

These are compiler checks, not capabilities of JSON Schema alone. Keep each diagnostic machine-readable: `code`, `file`, `line`, `column`, `config_path`, `operation_id`, and a safe explanation.

## 6. Configuration compilation and runtime publication

### 6.1 Compile pipeline

```text
Read bounded files
  -> strict YAML decode and structural schema validation
  -> resolve references and validate trust boundaries
  -> load OpenAPI / probe required MCP upstreams
  -> normalize native contracts and compute fingerprints
  -> resolve explicit exports and validate policy references
  -> compile input validators, REST plans and route indexes
  -> prepare pools, credential bindings and session factories
  -> construct candidate generation
  -> publish or discard the entire candidate
```

Compilation must be deterministic for fixed configuration, API documents, discovered capability manifests and policy inputs. Generate a revision from canonical normalized input plus capability fingerprints. Include secret **reference/version identifiers**, never raw secrets, in diagnostics or revision material.

Pin capability fingerprints over the public contract: name, description, schemas and relevant metadata. Changes to descriptions deserve review because they change what an agent is told a tool does. A background refresh may remove/quarantine a changed tool, but must not silently publish newly discovered capabilities.

### 6.2 State model

| Object | Lifetime | Sharing rule |
| --- | --- | --- |
| Compiled generation | From activation to last lease release | Read-only; maps and schema bytes cannot be mutated by consumers |
| Export identity | Stable across compatible reloads | Keyed by server ID and public tool name |
| Native manifest | Upstream/credential partition and fingerprint | Never merge privileged discovery into a less-privileged view |
| HTTP transport pool | Endpoint/network/TLS/proxy profile | Share sockets where safe; credentials remain request-local |
| MCP logical client/session | Upstream, identity partition, protocol and credential epoch | Never share across unrelated principals by default |
| Stdio process | Isolation key and process generation | Stateful handle, not an arbitrary reusable worker |
| Invocation | One execution attempt group | Owns its deadlines, audit context and resource leases |
| Cursor | Auth partition, catalog revision and short expiry | Signed; invalid after a relevant revision change |

### 6.3 Safe snapshot leasing

An atomic pointer swap alone does not make resource reclamation safe: a reader could load a pointer just before retirement closes its upstream handles. Use a store lock for the short acquire/publish critical section, plus explicit leases.

`Acquire()` takes a read lock, verifies the active generation is accepting leases, increments its reference count, and releases the lock. `Publish()` takes the write lock, switches the active generation, marks the previous one retired, and releases the lock. Close retired resources only when their generation and shared-resource reference counts reach zero, or after an explicitly enforced drain deadline. Make `Release()` idempotent.

Never hold the store lock while parsing, doing network I/O, invoking a tool or shutting down a subprocess. HTTP streaming, a legacy detached execution and any retained session handle must have explicit resource ownership; finishing an HTTP handler does not automatically mean the work has finished.

## 7. Core Go contracts

These are gateway-owned design interfaces, not SDK API signatures. The accompanying `reference/contracts.go` contains the fuller type contract and is dependency-free. Its compilation only checks that the reference types are valid Go; it does not implement the gateway.

```go
type RouteKey struct {
    ServerID   string
    PublicName string
}

type Route struct {
    Key RouteKey
    UpstreamID, NativeName, PolicyID string
    Contract ToolContract
    Behavior Behavior
    Timeout time.Duration
}

type ToolAdapter interface {
    Discover(context.Context, CredentialBinding) (Catalog, error)
    CallTool(context.Context, Call, EventSink) (Outcome, error)
    Close(context.Context) error
}

type SnapshotStore interface {
    Acquire() (SnapshotLease, error)
    Publish(context.Context, Snapshot) error
}

type Authorizer interface {
    Authorize(context.Context, Principal, Route) (Decision, error)
    RecheckAtDispatch(
        context.Context, Principal, Route, uint64,
    ) (Decision, error)
}
```

`Outcome` is a tagged union in the implementation: exactly one complete tool result, protocol error, or future input-required result. Validate this invariant at the adapter boundary. Do not force protocol errors into an `isError` result or serialize every result as text.

Carry tool arguments as validated `json.RawMessage` or a number-preserving representation. Avoid decoding every JSON number into `float64`; large identifiers and monetary decimal strings must not be silently changed. Copy mutable byte slices at ownership boundaries. Native MCP content blocks must retain their structured form through the adapter.

## 8. Routing and authorized discovery

### 8.1 Lookup algorithm

Compile `host + path -> virtual server` and `RouteKey -> Route` indexes. v1 supports exact paths only: no regex paths, user-controlled upstream names, or generic `call_any_url` tool. Validate the authority and path before entering an SDK handler; do not redirect a trailing slash for an authenticated MCP POST.

Resolve a public name using the current generation, then authorize the resolved export. The public name is never split to guess an upstream. For example, `issues.search` is a literal key whose route points to native tool `search_issues`; the dot has no runtime parsing meaning.

Every call follows the same policy path regardless of whether the client previously called `tools/list`. An unknown tool and a tool invisible to the caller return the same safe unknown-tool response, unless an explicitly configured OAuth scope challenge is appropriate. Log the precise internal cause without disclosing another tenant’s catalog.

### 8.2 Effective catalog

For each request, derive the visible catalog from:

```text
explicit exports
  intersect valid native contracts
  intersect enabled protocol/features
  intersect caller authorization
  intersect live revocation/quarantine overlay
```

The configured catalog may differ by authenticated principal. Never put one caller’s filtered result into a process-wide cache keyed only by virtual server. Keep ordering lexicographic by exact public name. Discovery is a local lookup over a prepared manifest, not a synchronous fan-out on every request.

v1 uses service-bound upstream discovery. A future delegated-credential implementation must discover or verify capabilities in the same credential partition used for execution. Do not import an administrator’s catalog and assume that it is an accurate user catalog.

### 8.3 Pagination and cursors

An opaque signed cursor contains format version, server ID, authorization partition digest, catalog revision, last emitted name and expiry. Bind its signature to the configured key ID. Use a bounded payload; do not embed credentials, schema bodies or the full principal.

Validate the signature, expiry, server, authorization partition and current catalog revision before continuing a page. On a relevant revision change, reject the cursor with an invalid-parameters response and instruct the client to restart listing. v1 does not retain entire historical catalogs solely to make old cursors work.

Filter first and paginate second, so page sizes do not reveal hidden entries. Materialize a small immutable catalog view for the duration of one list response. Recheck the authorization epoch before serialization; restart once on an epoch change, then return a bounded retryable failure instead of looping indefinitely.

Modern discovery responses include `ttlMs` and `cacheScope`; v1 uses `0` and `private` because catalogs are permission-sensitive. A zero client cache lifetime does not prohibit internal generation caching. [S07]

### 8.4 Replica selection and affinity

REST replicas must share the configured API contract. Choose healthy endpoints using smooth weighted round robin. Maintain circuit and inflight state per endpoint. A new attempt must satisfy the same policy, deadline and identity binding.

For remote MCP replicas, compare discovered native fingerprints before placing them in one eligible pool. An endpoint with incompatible tools is quarantined rather than receiving a random share of traffic.

Pin legacy upstream sessions to the selected endpoint. A server that keeps implicit conversational state cannot be moved to a different endpoint after a failure. v1 permits such integrations only through legacy downstream sessions with dedicated affinity; reject their exposure to modern clients. Explicit application handles are distinct from transport sessions and are not routing hints unless an operator-defined, validated routing policy exists.

## 9. REST OpenAPI compiler

### 9.1 Compilation product

Produce one `RESTPlan` for each exported operation:

```text
RESTPlan
  upstream and endpoint group
  HTTP method and immutable path-template segments
  parameter extractors and serializers
  selected request media type and body validator
  selected security requirement and credential binding
  response status/media validators
  normalized input schema and optional output schema
  timeout, retry classification and size limits
```

Use OpenAPI to describe operations, not to decide which network hosts are trusted. Ignore document `servers` values for destination selection; operator-configured endpoints are authoritative. A document cannot redirect execution, read arbitrary files through a reference, or install an authentication plugin.

### 9.2 Supported v1 subset

| Construct | v1 behavior |
| --- | --- |
| OpenAPI 3.0.x and 3.1.x documents | Accept only after selected operations pass the supported-subset compiler. |
| Operation selection | Require a unique `operationId` matching the export’s `native` value. |
| Path parameters | Required scalar values with `simple` serialization; reject structured path parameters. |
| Query parameters | Scalar values and flat arrays using `form`, with tested `explode` behavior. |
| Query objects, deepObject, matrix, label, space/pipe-delimited | Reject the selected operation with an explicit diagnostic. |
| Header/cookie parameters | Not exposed in v1; selected operations containing them require a later supported mapping. Authentication headers are handled separately. |
| Request bodies | One selected `application/json` representation; preserve omitted body versus explicit JSON null. |
| Multipart, form bodies, binary bodies, callbacks, webhooks | Reject selected unsupported request mappings. No background callbacks are synthesized. |
| Responses | JSON, bounded plain text and no-content responses; unsupported binary media become a safe tool failure. |
| References | Resolve document-local references; reject external references in v1. |

OpenAPI parameters have location-specific serialization rules. Preserve the declared serialization where supported and reject unsupported combinations rather than flattening every parameter into a query string. [S18]

### 9.3 Input schema construction

Use grouped arguments to prevent collisions between identically named parameters in different locations:

```json
{
  "path": {"user_id": "u_123"},
  "query": {"include_disabled": false},
  "body": {"display_name": "Example User"}
}
```

Only create groups used by the operation. Mark a group required when it contains required input or a required body. Set `additionalProperties: false` on gateway-created wrapper objects, but preserve the API schema’s own additional-property semantics inside the body.

Do not expose authorization headers, security-scheme API keys, destination hosts or HTTP methods as model arguments. Security requirements containing several schemes are conjunctive; alternative requirement objects are alternatives. Select one fully configured requirement rather than silently dropping part of it.

For OpenAPI 3.0, implement explicit conversion of nullable types and exclusive numeric bounds before compiling a JSON Schema validator. Preserve composition semantics; do not flatten `allOf` by overwriting properties. Strip response-only properties from generated request schemas only when their directionality is unambiguous. Reject transformations the compiler cannot justify. For 3.1, retain the dialect’s validation semantics instead of applying 3.0 conversions.

The gateway accepts JSON Schema 2020-12 as its baseline. Bound reference resolution, composition depth and total subschemas; do not automatically fetch network references. Unsupported dialects or unresolved references are compilation errors, never “accept any value” fallbacks. [S04]

Use limits of 64 nested schema levels and 10,000 subschema nodes as initial project defaults. These are engineering limits, not MCP limits. Validate them with recursive-schema and composition-heavy fixtures before release.

### 9.4 Path and query assembly

Compile templates into literal and parameter segments. Validate and encode each path value as one segment; never concatenate unescaped values and then normalize the resulting path. Reject dot segments, slash/backslash separators and unsafe percent-encoded equivalents under the v1 scalar-ID mapping. The fixture’s ID pattern intentionally allows only letters, digits, underscore and hyphen.

Preserve an endpoint’s base path: `/api/v1` plus `/users/{user_id}` must produce `/api/v1/users/u_123`, not `/users/u_123`. Test this explicitly because generic URL reference resolution can replace the base path.

Keep omission distinct from false, zero, empty string and explicit null. Encode query keys and values individually and preserve repeated keys for exploded arrays. Do not stringify nested objects using Go’s default map representation. Defaults in the OpenAPI document are descriptive unless a specific compiler rule explicitly materializes them; do not invent caller input.

## 10. REST execution and result translation

### 10.1 Request construction

After policy and admission succeed, select the endpoint, create a new HTTP request from the compiled plan, attach the call deadline, and resolve its upstream credential. Use fresh request headers. Do not mutate a shared `http.Client`, cookie jar or default authorization map for each user.

Validate DNS results against the endpoint network policy immediately before dialing and connect to the validated address while retaining the intended TLS server name. Disable environment-derived proxies and HTTP redirects by default. Do not carry downstream `Authorization`, `Cookie`, `Host`, `Forwarded` or hop-by-hop headers into the upstream request.

The credential broker injects only the configured upstream credential. Trace propagation uses an allowlist. Business correlation or tenant headers require an explicitly designed trusted mapping, not a passthrough of similarly named client headers.

### 10.2 Success envelope

REST tools return a gateway-owned object envelope. This keeps array/scalar API results compatible with the object-oriented structured-result restrictions of the legacy tool contract. Existing MCP tools are treated separately and are not automatically wrapped. [S16]

```json
{
  "ok": true,
  "status": 200,
  "data": {
    "id": "u_123",
    "display_name": "Example User"
  }
}
```

Set the MCP result’s `structuredContent` to the envelope and include a compact JSON text representation. For HTTP 204, use `data: null`. HTTP 202 means that the API accepted the request, not that the underlying job completed; return its actual status and body without inventing an MCP task.

Generate an output schema only when the compiler can describe all emitted success and error envelopes accurately. Otherwise omit `outputSchema` and retain response validation against the applicable API response schema. Never advertise a schema the normalized result can violate.

### 10.3 Failures and limits

A REST business failure is a completed tool execution with `isError: true`, not a successful tool result and not necessarily an HTTP failure of the gateway. Return a safe structured error with a stable gateway code. Do not forward arbitrary response headers, HTML error pages, stack traces, tokens or internal endpoint addresses.

```json
{
  "ok": false,
  "error": {
    "code": "UPSTREAM_OUTCOME_UNKNOWN",
    "message": "The request may have reached the service.",
    "execution_id": "exec_example_001",
    "retryable": false,
    "outcome": "unknown"
  }
}
```

Apply limits to compressed input, decompressed body, parsed representation, and final encoded MCP output. Include both the structured and text copy when calculating final size. An oversized result is not silently truncated: return `RESULT_TOO_LARGE` and record whether the operation had already completed.

Reserve a conservative buffer budget before dispatching a buffered request, including input, decoded upstream response and output encoding. Reservations belong to the global byte limiter, not just to per-request limits. Read with a limit-plus-one strategy and close every response body. A memory-budget wait is part of the call’s admission deadline.

## 11. Existing MCP upstream adapter

### 11.1 Capability discovery

Use an independent MCP client for the upstream boundary. Probe `server/discover` first with a short discovery deadline; select a modern version or perform the documented legacy fallback. Only discovery-safe methods may trigger protocol auto-detection. Never probe compatibility by first executing a mutating tool. Version errors, authentication errors and header-validation errors must not be conflated with evidence of a legacy server. [S02, S06]

For a legacy upstream, complete initialization and its initialized notification before listing tools. [S15] Follow all upstream list pages with caps on pages, tools, bytes and duration. Detect repeated cursors and duplicate native names. Initial caps are 100 pages and 10,000 tools per credential partition; exceeding them is a discovery failure.

Store the native name separately from the exported name. Validate schema dialects and header annotations before accepting a tool. Do not promote upstream descriptions, annotations or instructions into gateway security configuration.

### 11.2 Invocation translation

Resolve the public route, substitute only the mapped native name, and preserve validated business arguments. Generate an upstream request ID distinct from the downstream ID. Rebuild upstream protocol metadata for the selected version and use the gateway as the upstream MCP client identity; caller identity is an authorization concern, not a trusted `clientInfo` string.

Rewrite protocol metadata headers from the rewritten body, including native `Mcp-Name` and any applicable parameter mirrors. Do not forward a public name in a header while sending a native name in the body. Preserve unknown valid MCP parameter-mirror headers where the current transport requires intermediary forwarding, subject to overall header limits; they never bypass body validation or confer authorization. [S03]

Return native content blocks, `isError`, structured content and negotiated metadata without converting everything into a text string. Reject malformed upstream messages as `UPSTREAM_PROTOCOL_ERROR`. Preserve valid upstream JSON-RPC errors as protocol errors, with identifiers rewritten and sensitive details sanitized.

### 11.3 Cross-version result strategy

Modern ordinary results encode `resultType: "complete"`; a legacy ordinary result can be interpreted as complete when translated to the modern side. Do not treat an interim `input_required` result as a completed tool call. [S23]

For modern upstream tools whose structured output cannot fit the legacy result contract, the v1 legacy catalog must omit those exports. Do not silently wrap native MCP output and change its output schema. The same virtual server may therefore present a protocol-compatible subset to a legacy client. Validate this distinction during discovery and test it with array/scalar output fixtures.

Capabilities forwarded upstream are the intersection of downstream capability support, gateway bridge support and route policy. v1 advertises no interactive client features. A tool requiring unsupported interaction fails explicitly; a future full bridge is specified in Section 19.

### 11.4 Catalog refresh policy

Poll on the configured interval with jitter. Additions remain unexported. An unchanged contract refreshes its validity time. A removed or changed exported contract is quarantined for new calls and appears in admin diagnostics until an operator validates a new generation.

A network refresh failure alone does not immediately delete a known catalog. Keep its last accepted contract up to `max_stale`, then quarantine it. A call still fails safely when the endpoint is unavailable. Revocation and contract-drift quarantine are live overlays, not mutations of a leased route object.

## 12. Stdio process supervision

The stdio binding carries newline-framed JSON-RPC through a launched subprocess; protocol output and diagnostic stderr are separate. Implement the framing and lifecycle through the selected SDK where possible. [S11]

Spawn a configured absolute executable with a fixed argument vector. Do not use a shell, substitute tool arguments into commands, inherit the entire gateway environment, run package installers on request, or download code during a tool invocation.

Use one stdout protocol reader and a serialized stdin writer per process. Continuously drain bounded stderr so a noisy child cannot deadlock on a full pipe. Configure frame limits explicitly rather than accepting a default scanner token size. Treat malformed stdout or an oversized frame as a process protocol failure, not as text tool output.

Isolation key:

```text
upstream configuration generation
+ tenant
+ subject / OAuth client partition
+ credential epoch
+ downstream session, when implicit state requires it
```

A `per_principal` process partition prevents cross-principal memory sharing but does not reduce the OS permissions of a child. Run untrusted or sensitive upstreams under dedicated users/containers, with read-only mounts where practical, no privilege escalation, restricted networking and a constrained writable directory. The gateway is not a sandbox by itself.

State machine:

```text
STOPPED -> STARTING -> DISCOVERING -> READY
                    -> FAILED -> BACKOFF -> STARTING
READY -> DRAINING -> STOPPED
READY -> CRASHED -> BACKOFF
```

Limit total processes, startup attempts and per-process inflight calls. Default to one call at a time until an upstream is explicitly qualified for concurrency. Apply exponential restart backoff with jitter and a ceiling. Do not automatically restart a child and replay unfinished tool calls.

A tool timeout must cancel that call, not kill a shared process holding unrelated calls. If cancellation cannot release a hung, single-inflight process, mark it unhealthy, terminate it after a supervisor grace period, and report the affected execution’s outcome as unknown where appropriate.

During shutdown, stop new leases, close stdin, wait a bounded interval, send termination, then kill the process group only if necessary. Reap the process exactly once and close all pipes.

## 13. Downstream protocol implementation

### 13.1 HTTP edge and modern facade

Perform connection/header limits, host/origin checks, resource routing and authentication before expensive schema validation or discovery. Read a bounded body; reject malformed JSON and duplicate object members that would create ambiguous routing.

Modern protocol validation checks the request metadata and its HTTP mirrors. Missing required per-request version/capabilities is a malformed request; body/header disagreement is not a routing hint. Use the SDK’s defined error constants and wire shapes. Unknown modern RPC methods use the transport’s specified HTTP 404 plus JSON-RPC method-not-found error. [S03, S04]

The example request in the companion `fixtures/tools-call.modern.http` contains the complete headers and per-request metadata. The gateway replaces those fields independently on the upstream hop; it does not assume both endpoints use the same protocol era.

### 13.2 Legacy facade

Isolate `initialize`, `notifications/initialized`, legacy `ping`, cancellation and optional session management behind the legacy facade. Do not accept a legacy initialization request as a modern request with missing metadata. v1 issues gateway-owned legacy sessions so request IDs and any affinity have an explicit ownership boundary.

Bind each session to its virtual server, authenticated issuer/subject/tenant/client partition, chosen protocol and capability set. Authenticate every later HTTP request independently; the session ID is never a bearer credential. A token refresh for the same principal can continue the session only if current authorization still allows it.

v1 does not offer a standalone GET SSE channel or message replay; return the permitted 405 for unsupported GET streaming. An unknown/expired gateway session returns the legacy session-expired behavior, requiring reinitialization. No current-protocol session ID or upstream session ID is exposed as a substitute. [S14]

For legacy requests, disconnecting the POST does not itself cancel upstream execution. Detach the execution lifetime from the HTTP response lifetime while retaining a bounded execution deadline and explicit cancellation mapping. The gateway does not promise delivery of a lost response; report this limitation and rely on application idempotency for caller-initiated recovery.

### 13.3 Protocol limits

Reject request batches in v1; both supported wire contracts expect individual messages for this transport path. Limit header bytes, JSON depth, request bytes, concurrent streams and parsing time. For long-running tool calls with progress, use request-scoped SSE with immediate flush, bounded event queues and a final response.

Use separate timeouts for short request parsing and long response execution. A blanket 30-second HTTP write timeout must not accidentally cut off a permitted long-lived stream. v1 has no list-change subscriptions; if a later release enables them, use a separate stream budget and heartbeat policy.

## 14. Tool-call execution lifecycle

### 14.1 Ordered algorithm

```text
1. Validate edge framing, virtual server and protocol era.
2. Authenticate and construct a trusted Principal.
3. Acquire the active snapshot lease.
4. Resolve the exact public tool name.
5. Check visibility, feature compatibility and authorization.
6. Validate arguments against that route's compiled schema.
7. Derive deadline and request/identity partition.
8. Acquire admission and buffer permits within queue timeout.
9. Bind upstream credentials and acquire an adapter lease.
10. Recheck live policy, quarantine and credential epochs.
11. Record the dispatch audit event.
12. Select eligible endpoint and issue the native request.
13. Forward permitted progress with bounded backpressure.
14. Validate/translate the outcome for the downstream version.
15. Record completion, failure or unknown outcome.
16. Release response buffers, adapter, permits and snapshot.
```

Every acquired resource has exactly one release path, including validation errors, panic recovery, cancellation and disconnected legacy clients. Model state transitions explicitly rather than attaching cleanup to successful returns only.

Execution states are `RECEIVED`, `AUTHORIZED`, `QUEUED`, `DISPATCHING`, `RUNNING`, and a terminal `COMPLETED`, `FAILED`, `CANCELLED` or `OUTCOME_UNKNOWN`. Authentication or validation rejection happens before any upstream work. Cancellation describes a stop request or local lifecycle outcome; it is not proof that upstream business effects were rolled back.

### 14.2 Correlation

Maintain separate identifiers for the HTTP request, JSON-RPC request, gateway execution, upstream request and tracing span. A downstream request ID is scoped to its connection/session/request context; never use it alone as a global cancellation key.

For modern HTTP, use the request’s context for cancellation. For modern-to-legacy upstream translation, map that cancellation to the legacy upstream request ID. For legacy downstream calls, validate the principal/session before applying `notifications/cancelled` to the stored execution handle.

Modern stream closure is a cancellation signal, but stopping execution remains best effort. Record a response-unknown state whenever dispatch occurred and a definitive outcome was not received. [S10]

### 14.3 Representative call

```text
Agent         Gateway           REST adapter          Users API
  | tools/call   |                    |                    |
  |------------->| authenticate      |                    |
  |              | route + policy   |                    |
  |              | validate         |                    |
  |              |----------------->| GET compiled path  |
  |              |                  |------------------->|
  |              |                  |      200 JSON      |
  |              |                  |<-------------------|
  |              | normalized result|                    |
  |              |<-----------------|                    |
  | MCP result   | audit + release   |                    |
  |<-------------|                    |                    |
```

## 15. Errors, retries and side-effect safety

### 15.1 Error classification

| Condition | Downstream behavior | Upstream call? |
| --- | --- | --- |
| Invalid host/origin or oversized HTTP input | HTTP rejection; appropriate safe transport error | No |
| Missing/invalid gateway access token | HTTP 401 with the gateway resource challenge | No |
| Known missing required gateway scope | HTTP 403 and applicable scope challenge before streaming | No |
| Invalid JSON-RPC envelope / malformed parameters | Version-correct protocol error | No |
| Unknown or concealed tool | Invalid-parameters unknown-tool response | No |
| Known tool, invalid business arguments | Actionable tool error with safe validation details | No |
| Queue full or rate limited before dispatch | HTTP 429 or a version-appropriate bounded admission error | No |
| Upstream business failure, such as REST 404/409/422 | Completed tool result with `isError: true` | Yes |
| Invalid upstream credential | Tool error `UPSTREAM_AUTH_FAILED`; never challenge as the downstream issuer | Possibly |
| Valid upstream JSON-RPC error | Preserve protocol-error category and rewrite its request ID | Yes |
| Timeout/disconnect after dispatch | Tool failure with explicit unknown outcome when response is still deliverable | Possibly completed |
| Unsupported or malformed upstream content | `UPSTREAM_PROTOCOL_ERROR` or `RESULT_UNSUPPORTED` | Yes |
| Oversized output after a completed action | `RESULT_TOO_LARGE`; indicate that execution may already have completed | Yes |

The current tools specification distinguishes protocol failures from actionable tool-execution failures, including business input validation. Keep those categories separate instead of returning one generic HTTP 500 for all cases. [S05]

Never return a fabricated success after a timeout. Once an SSE response has started, do not try to replace it with an HTTP 401 or another status; finish with a permitted protocol/result error or close the stream as appropriate.

### 15.2 Retry policy

Default `max_attempts` is 1. v1 permits extra explicit attempts only for operator-reviewed read-only tools and only when the endpoint failure/status is in the configured internal retry class. Do not retry authorization failures, validation failures, arbitrary `isError` results or protocol-incompatible responses.

Do not infer safety from `readOnlyHint`, `idempotentHint`, a tool’s name, or HTTP GET alone. Gateway policy is the authority, supported by an upstream contract. v1 performs no automatic retries for `mutating`, `unknown` or merely asserted idempotent write operations.

If enabled for reads, use exponential delay with full jitter, bounded by the remaining end-to-end deadline; respect bounded upstream retry guidance. A new attempt must recheck revocation and reuse only the intended identity binding.

Audit lower-level transport behavior: Go’s HTTP transport can perform limited retries for requests it regards as idempotent. Therefore `max_attempts` counts gateway-controlled attempts, not an absolute promise that no network-level retransmission can occur. Ensure non-replayable writes do not accidentally acquire replay-enabling body or header settings. [S21]

### 15.3 Idempotency boundary

An MCP JSON-RPC request ID is not an idempotency key. A process-local deduplication map cannot provide exactly-once execution across restarts or replicas. The gateway makes **no exactly-once guarantee**.

A future write-retry feature requires an upstream-documented idempotency contract, a stable caller-supplied operation key bound to principal/route/payload, and a defined retention/reconciliation policy. Do not implement it by generating a new key for each retry. For payments or other important writes, require callers to reconcile an unknown result against upstream business state before submitting again.

## 16. Concurrency, timeouts and health

Apply admission in a documented order: global request budget, per-principal rate limit, route/upstream inflight budget, buffer reservation, then endpoint connection acquisition. Never hold a scarce execution slot while waiting indefinitely for another limiter. Queue waits and credential acquisition consume the same end-to-end deadline.

The effective tool deadline is the minimum of gateway limit, export/upstream limit, server-drain deadline and an authenticated, bounded caller hint if one is supported. Untrusted arguments cannot extend it. Progress does not reset the hard maximum.

| Budget | Initial project default |
| --- | --- |
| Active requests / queued requests | 256 / 64 |
| Queue wait / ordinary call timeout | 250 ms / 30 s |
| Input / encoded result / source schema | 1 MiB / 4 MiB / 2 MiB |
| Shared buffering budget | 256 MiB, reserved conservatively before dispatch |
| Per-principal admission | 20 requests/second with burst 40, per process |
| Discovery work | At most 8 concurrent upstream refreshes |
| Protocol/schema nesting | 64 levels, with separate node/byte caps |
| Legacy sessions | At most 1,000; idle expiry 15 minutes |

These defaults are starting constraints, not measured capacity claims. Per-principal limiter maps and session stores need bounded key counts and idle eviction; otherwise an attacker can exhaust memory by manufacturing identifiers.

Use circuit state `CLOSED -> OPEN -> HALF_OPEN -> CLOSED`. Count transport failures and designated availability failures, not ordinary business errors. Probe HTTP readiness only through operator-approved non-mutating health checks. Modern MCP health uses bounded discovery where appropriate; do not send a removed RPC such as legacy `ping` to every modern server. [S23]

For streams, cap queued progress bytes/events. Coalesce intermediate progress only when doing so preserves required semantics. A slow client must not cause unbounded allocation; on persistent backpressure, terminate according to the selected protocol era and record the execution outcome.

## 17. Authentication, authorization and trust boundaries

### 17.1 Downstream OAuth resource server

Integrate with an existing authorization server rather than issuing a new identity system inside the gateway. Publish OAuth protected-resource metadata for each virtual server and its associated authorization server. An access-token verifier alone is not the full discovery contract expected by general remote MCP clients. [S12]

For the example resource `/mcp/engineering`, serve the corresponding protected-resource metadata location and include its address in a 401 `WWW-Authenticate` challenge. Bind verification to the configured resource URI, issuer and algorithm allowlist. Enforce signature, audience, expiry, not-before and required claim validation. Cache verified issuer keys with bounded refresh; never follow a token-supplied arbitrary key URL.

Build `Principal` only from validated claims or a separately authenticated trusted identity integration. `params._meta.clientInfo`, an `X-Tenant-ID` header and tool arguments are not identity proofs. Derive tenant, subject and scopes once; do not let later request transforms overwrite them.

### 17.2 Separate upstream credentials

Never blindly forward a gateway access token to an upstream. Upstream bearer tokens, OAuth audiences and service grants are separate security domains. The MCP security guidance explicitly identifies unsafe token passthrough and confused-deputy behavior. [S13]

In v1, upstream credentials are configured service bindings. Every export requires an explicit acknowledgement of that execution identity. Per-principal MCP sessions isolate state, but they do not turn a shared service token into a delegated user token.

Store secrets in narrowly scoped resolvers, not in route definitions returned to administration clients. Resolve references at candidate preparation, maintain an opaque credential epoch, and redact values from logging, tracing, panics, SDK diagnostics and error bodies. Rotation invalidates affected logical sessions and discovery partitions; it does not mutate another caller’s active HTTP headers.

A future delegated broker must bind credentials to issuer, principal, tenant, client application, target resource, scopes and consent grant. Durable refresh tokens and consent require an explicit secure persistence choice. Those features cannot retain the same “memory only, no durability” promise as the standalone v1 service-credential model.

### 17.3 Policy evaluation

Each export refers to exactly one named policy. All policy conditions must pass; there is no wildcard fallback. Evaluate before argument processing that might reveal hidden schemas and again immediately before dispatch. Scope rules authorize the **tool**, not automatically every object identifier accepted by it.

For sensitive resources, use upstream-enforced object authorization or a separately reviewed argument-level policy module. Do not claim that an allowlisted `get_user` tool with a privileged service credential prevents access to every other user record. The initial example intentionally documents service-level rather than delegated access.

Tool annotations are descriptive hints, not authorization, approval or retry guarantees. v1 provides no independent human-approval service. Do not expose high-impact mutations under the assumption that a tool description saying “ask first” enforces consent. A later approval mechanism must use a trusted, signed approval grant bound to the exact principal, export, arguments and policy revision.

### 17.4 Network and filesystem controls

Treat upstream responses, API documents and discovered schemas as untrusted inputs. Apply SSRF controls to every outbound mechanism, including JWKS fetches, discovery, redirects, credential exchange and schema references—not only to business calls. Explicitly allow private API destinations only where configured; never allow arbitrary link-local/metadata endpoints.

The dialer must check the actual resolved address, not just the original hostname, and maintain TLS hostname verification. Revalidate allowed destinations after DNS changes. Disable redirects in v1; never retain credentials across an unreviewed authority change.

Remote MCP tool results may contain resource links. Preserve permitted content as data, but do not automatically fetch linked URLs or local paths. Upstream text is not an instruction to the gateway to change configuration, run a command, grant a scope or reveal credentials.

Keep configuration and child executables outside writable directories accessible to gateway callers. Explicit operator access is required to change executables, arguments, credential references or network policy. The administration interface is a privileged control boundary, not another public MCP tool family.

## 18. Startup, reload, revocation and shutdown

### 18.1 Startup

Start a minimal local diagnostic listener in a not-ready state. Load/validate sources, prepare credentials, probe required upstreams, compile the initial generation, bind the data listener and mark readiness only after activation. Fail with a nonzero exit code if required contracts or security settings cannot be prepared within the startup budget.

Required means a contract and execution path must be established, not that a forever-running remote service can be guaranteed healthy. Readiness subsequently reflects required upstream availability and security validity; liveness only reflects the gateway’s own ability to run.

An unavailable optional upstream contributes dormant exports rather than invented schemas. Its absence must appear in diagnostics. First activation of a previously unknown optional contract requires an explicit successful reload; a background refresh does not publish an unreviewed first schema.

For service-bound MCP integrations, an isolated discovery client/process may obtain a static native manifest. Never reuse a privileged probe process as a user execution process. Per-principal execution clients are created lazily and must verify the expected contract before serving calls.

### 18.2 Reload transaction

```text
SIGHUP
  -> serialize reload requests
  -> compile candidate under startup budget
  -> compare accepted native contracts and listener constraints
  -> prepare resources without touching the active generation
  -> publish new generation and authorization epoch
  -> notify local administrative observers
  -> drain retired resources within the configured grace period
```

On candidate failure, close candidate-only resources and keep the last working generation active. Record a redacted diagnostic and increment a reload-failure metric. Coalesce repeated reload signals; do not run unlimited candidate compilations concurrently.

Listener address changes and other restart-only fields must produce a clear `RESTART_REQUIRED` diagnostic rather than a partial reload. v1 hot reload supports exports, policies, credentials, upstream configurations and accepted capability manifests within the existing listener topology.

Every new call uses the currently active route generation, even if it arrives through an older legacy session. Legacy protocol state is separate from configuration state. Reconcile sessions with removed servers, changed identity bindings or retired implicit-state upstreams; close or expire them rather than allowing stale permissions indefinitely.

### 18.3 Emergency revocation

Maintain a small, separately published deny/quarantine overlay keyed by principal, export or upstream. A normal route lease preserves structural consistency, but it must not suppress emergency revocation until a long request finishes.

The dispatch gate reads the current authorization/quarantine epoch immediately before making an upstream request. Updating the epoch prevents not-yet-dispatched executions from proceeding. Already-dispatched work can only be cancelled best effort; the gateway cannot revoke a business action retroactively.

Do not claim an impossible zero-race rollback guarantee. Define the linearization point as the dispatch authorization decision and audit its policy epoch. A revocation feature that must serialize with that boundary needs a shared dispatch gate, not a check performed several asynchronous steps earlier.

### 18.4 Shutdown

Mark readiness false, stop new admission, cancel discovery/reload jobs, and allow active executions to drain up to `drain_timeout`. Close idle sessions and transports, then cancel remaining executions with version-correct semantics. Stop/reap stdio children and flush bounded telemetry.

Flush failures must not indefinitely delay shutdown. Preserve dispatch/completion facts where the configured sink allows it, but do not claim durable audit delivery from buffered stdout alone. Exit nonzero on critical internal failures and document whether the supervisor should restart the process.

## 19. Extension architecture: not enabled in v1

### 19.1 Resources and prompts

Add separate `ResourceAdapter` and `PromptAdapter` interfaces for list/read/templates and list/get operations. Do not overload the tool-call interface. Allocate names and policy rules independently for each capability family.

For resources, use a gateway-owned URI mapping that encodes the virtual server, upstream and native URI losslessly. Parsing an encoded URI identifies only a candidate route; re-authorize it against the caller and an allowed resource contract before access. Never turn a decoded arbitrary HTTP or `file:` URI into a direct fetch.

Resource links returned by a tool may refer to resources not present in a list, so future routing must support validated dynamic native URIs rather than requiring every resource instance to have been pre-listed. Preserve the owning upstream and enforce read policy on every access. [S05]

Prompt composition may include resource references; rewrite those through the same URI ownership layer. Optional argument completion is a separate method with its own visibility rules. Publish a feature only when list, execution, authorization, content translation and error paths all exist.

### 19.2 Modern interactive bridge

MCP 2026-07-28 represents requests for additional client input as an `input_required` result, followed by a new client request carrying the response/state. This is not a normal retry after a network failure. [S09]

For a modern-to-modern bridge, preserve the input request semantics and bind the upstream’s opaque state into a gateway continuation envelope. Encrypt and authenticate that envelope with an approved AEAD implementation. Include route identity, endpoint affinity, credential partition, original argument digest, schema revision, expiry and upstream state. Re-authorize every continuation.

Limit continuation size and lifetime; reject altered arguments, another principal’s continuation or an incompatible route revision. Continuations may keep an explicitly retained resource generation until their bounded expiry. Never forward a continuation to a different upstream based only on the new request’s public tool name.

A stateless envelope cannot by itself enforce single-use execution across crashes or replicas. Require upstream replay safety or an optional durable anti-replay store for workflows needing that guarantee.

Cross-era interactive bridging is a separate project: translating a blocking legacy callback into modern multi-round-trip behavior requires retained suspended execution state, not a simple field rename. Until implemented and tested, reject it explicitly.

### 19.3 Subscriptions and tasks

Modern list/resource notifications use the request-based subscription pattern rather than the removed standalone GET stream. A later subscription manager must own its filters, authorization checks, upstream subscriptions, cancellation and per-listener quotas. [S08]

Tasks are an optional extension in the current specification, not a reason to treat every HTTP 202 response as an asynchronous MCP task. [S23] Supporting durable tasks or durable approvals requires explicit persistence, restart recovery, expiration and ownership semantics. None is silently included in the v1 memory-only runtime.

## 20. CLI and administration contracts

The following commands are **proposed interfaces to implement**, not commands provided by the reference kit:

```bash
mcpgate validate --config ./gateway.yaml
mcpgate validate --config ./gateway.yaml --online
mcpgate inspect --config ./gateway.yaml --format json
mcpgate serve --config ./gateway.yaml
mcpgate version --json
```

Offline validation checks the schema, local API documents, references and compile-time rules without contacting MCP upstreams. It reports unresolved online contracts rather than claiming runtime readiness. Online validation also probes upstream capabilities and credentials with safe discovery methods.

`inspect` returns redacted exports, methods, policies, native fingerprints, protocol compatibility and diagnostic state. It must never print secret values or resolved bearer headers. `version` reports executable version, exact Go/SDK build versions, supported configuration version and tested MCP versions.

Reload is delivered by `SIGHUP` to the supervised process. Do not expose a public unauthenticated reload endpoint. Local status endpoints:

| Administrative endpoint | Contract |
| --- | --- |
| `GET /livez` | 200 only when the process event loop/runtime is live |
| `GET /readyz` | 200 only when a valid generation and required dependencies are ready |
| `GET /v1/status` | Active revision, readiness reasons and resource counts |
| `GET /v1/upstreams` | Redacted endpoint/contract/health state |
| `GET /v1/routes` | Compiled export and policy references, not secrets |
| `GET /metrics` | Bounded-cardinality runtime and execution metrics |

Bind administration to loopback or a restricted Unix socket in a later deployment profile. Debug profiling is disabled by default. Health endpoints must not call a mutating upstream tool to prove availability.

## 21. Observability and audit model

Emit one admission/dispatch event and one terminal event per accepted execution where possible. Required fields: timestamp, execution ID, trace ID, server, public tool, native operation, pseudonymous principal partition, upstream/endpoint ID, route revision, policy epoch, attempt count, durations, result category and outcome certainty.

Do not log raw arguments, tool output, tokens, session identifiers or continuation envelopes by default. Store a keyed pseudonymous principal digest rather than a plain low-entropy hash. Redaction must apply to SDK logs and child stderr as well as application logs.

Recommended metric names:

```text
mcpgate_requests_total{server,method,outcome}
mcpgate_tool_duration_seconds{server,tool,upstream}
mcpgate_inflight{upstream}
mcpgate_queue_wait_seconds{upstream}
mcpgate_rejections_total{reason}
mcpgate_upstream_failures_total{upstream,category}
mcpgate_reload_total{result}
mcpgate_catalog_quarantined{upstream}
mcpgate_legacy_sessions
mcpgate_stdio_processes{upstream,state}
mcpgate_buffer_reserved_bytes
```

Never use user ID, tenant ID, raw URL, request ID, arguments or cursor as a metric label. Bound even configured tool labels through catalog limits. Put high-cardinality investigation data in access-controlled logs instead.

Create trace spans for validation, queueing, credentials, upstream execution and normalization. Forward only reviewed trace metadata; arbitrary baggage can leak data. A backend application’s own request/reference ID may be recorded only through an allowlisted response field.

Stdout logging provides operational evidence but not a tamper-proof ledger. Strong durable auditing is a deployment feature requiring a durable sink and an explicit policy for what to do when that sink is unavailable.

## 22. Deployment and operational model

### 22.1 Standalone binary

Run `mcpgate` as a supervised, unprivileged service with read-only configuration and separate secret files. Keep the existing application untouched. Place a trusted TLS ingress in front or add an independently tested TLS-listener profile; do not expose cleartext bearer-token traffic to an untrusted network.

There is no data migration for v1. Restart loses in-memory sessions, rate-limit counters and discovery freshness. The gateway reloads configuration and rediscovers contracts. Upstream business effects are not lost or undone merely because the gateway restarted.

### 22.2 Container or sidecar

Mount configuration read-only. Do not bake tokens into images. Drop privileges and unnecessary capabilities, use a read-only root filesystem, set process/memory limits and provide only the writable directories the gateway or child processes require.

An image intended only for REST/remote MCP does not need a shell or package manager. A stdio deployment must include its explicitly approved child executable and OS dependencies. A minimal image that lacks them is not a working stdio runtime.

A sidecar shares the application’s deployment unit, not automatically its identity or security permissions. Avoid mounting application secrets that the gateway does not need.

### 22.3 Replication

Pure modern request-scoped calls can be spread across gateway replicas when their configuration, policy and upstream identity bindings are equivalent. That does not make stdio processes, legacy sessions, implicit upstream state or local counters globally shared.

Use affinity for legacy sessions, or run those virtual servers on one instance until an explicit session-routing design exists. Do not promise failover by silently recreating an expired upstream session and replaying its last write.

Rate limits are per instance. A cluster-wide quota needs an upstream limiter, coordinated control plane or optional shared backend. Consistent hashing can distribute load but is not durable state replication. Distribute config and cursor keys through existing deployment mechanisms and record each instance’s revision in diagnostics.

## 23. Test plan and release acceptance criteria

Every case below is a required implementation test unless marked as an extension gate. Use deterministic fixture upstreams, injected clocks, controllable network failures and a recording HTTP/MCP server. These are requirements for the future gateway; the reference kit itself does not claim to have passed them.

### 23.1 Configuration, compiler and REST tests

| ID | Scenario | Required result |
| --- | --- | --- |
| CFG-01 | Duplicate YAML key or unknown field | Reject with source location; no listeners become ready |
| CFG-02 | Missing policy, secret or upstream reference | Reject candidate with redacted diagnostic |
| CFG-03 | Duplicate tool name or server path | Reject instead of last-write-wins behavior |
| CFG-04 | External schema reference or source-root escape | No network/file access; compilation fails |
| CFG-05 | Optional unavailable upstream | Ready only if required dependencies pass; dormant exports omitted |
| CFG-06 | Failed online reload | Current revision and its working requests remain intact |
| CFG-07 | Modern export of implicit legacy state | Reject incompatible configuration |
| CFG-08 | Advanced feature flag enabled in v1 | Structural validation fails |
| CFG-09 | Listener-address change during hot reload | Explicit restart-required diagnostic; no partial activation |
| CFG-10 | Secret value appears in a candidate failure | Redacted from diagnostics, revisions and logs |
| REST-01 | Sample `getUser` call | Correct method, base path, ID encoding and credential binding |
| REST-02 | Same parameter name in path/query/body | Separate groups preserve each value |
| REST-03 | False, zero, empty, omitted and null | No conflation or unexpected default injection |
| REST-04 | Flat query array, explode on/off | Exact expected query bytes for each supported case |
| REST-05 | Unsupported parameter serialization | Compile-time failure, not malformed runtime HTTP |
| REST-06 | Large integer/decimal string | No numeric precision loss |
| REST-07 | Path traversal or separator injection | Rejected before network dispatch |
| REST-08 | JSON, text, 204 and 202 responses | Correct envelope; no fabricated job completion |
| REST-09 | Business 404/409/422 and upstream 401 | Correct error category; no leaked credentials/challenges |
| REST-10 | Compressed bomb or oversized output | Bounded memory and explicit size failure |
| REST-11 | Response schema violation | Safe error; no falsely schema-conforming output |
| REST-12 | Misconfigured OpenAPI security combination | Reject operation rather than drop a required scheme |

### 23.2 Protocol, isolation and security tests

| ID | Scenario | Required result |
| --- | --- | --- |
| MCP-01 | Modern client with full request metadata | Discovery and tool call succeed without initialization |
| MCP-02 | Missing or mismatched header/body metadata | Version-correct rejection and zero upstream calls |
| MCP-03 | Legacy client lifecycle | Initialization/session behavior is separate from modern facade |
| MCP-04 | Modern client to legacy MCP upstream | Safe detection before tools execute; correct name/result translation |
| MCP-05 | Legacy client to modern object-output tool | Compatible catalog and correct legacy result encoding |
| MCP-06 | Modern array-output tool viewed by legacy client | Incompatible export omitted rather than silently transformed |
| MCP-07 | MCP tools with colliding native names | Explicit aliases route to the intended upstream only |
| MCP-08 | Repeated discovery cursor or incompatible replica | Bounded failure and quarantine |
| MCP-09 | Image/audio/resource content block | Native content preserved within advertised support and limits |
| MCP-10 | Request-scoped progress and slow consumer | Correct correlation and bounded backpressure |
| MCP-11 | Tool requires an unadvertised interactive feature | Explicit feature failure, never false completion |
| MCP-12 | Unknown modern RPC method | HTTP 404 with method-not-found JSON-RPC error |
| MCP-13 | Auto-detection meets an ambiguous legacy error | Only safe discovery is retried; no mutating probe |
| MCP-14 | Modern catalog response after policy change | Private zero-TTL hints and newly filtered contents |
| AUTH-01 | Wrong issuer/audience, expired or unsigned token | No catalog or execution access |
| AUTH-02 | Guessed hidden public tool name | No authorization bypass or cross-tenant schema leak |
| AUTH-03 | Two users share one HTTP pool | No header, cookie or credential contamination |
| AUTH-04 | Same request ID in two legacy sessions | Cancellation affects only its owning principal/session |
| AUTH-05 | Replayed cursor from another tenant or revision | Rejected; no hidden page entries leaked |
| AUTH-06 | DNS resolves to disallowed/link-local address | Connection blocked even when hostname looked acceptable |
| AUTH-07 | Tool result requests credential/config changes | Treated only as untrusted output data |
| AUTH-08 | Child command/argument/environment injection | Cannot alter operator-selected process launch |
| AUTH-09 | Policy revoked during queue wait | Dispatch gate blocks the still-unsent call |
| AUTH-10 | Credential rotates while another call is active | New partition/epoch; no shared-header mutation |
| AUTH-11 | Valid session ID supplied with another principal | Reject independently of session existence |

### 23.3 Lifecycle and reliability tests

| ID | Scenario | Required result |
| --- | --- | --- |
| REL-01 | Reload overlaps request acquisition | No use-after-close, race, deadlock or mixed route generation |
| REL-02 | Long-running call during resource retirement | Lease honored or explicitly cancelled after drain deadline |
| REL-03 | Upstream write commits then connection drops | No automatic replay; unknown outcome recorded |
| REL-04 | Read-only retry under a shrinking deadline | Bounded attempts, correct backoff and final cleanup |
| REL-05 | Modern client closes response stream | Upstream cancellation requested; no claim of rollback |
| REL-06 | Legacy client disconnects without cancellation | Execution remains bounded but is not cancelled merely by disconnect |
| REL-07 | Stdio stderr floods or stdout frame is huge | Bounded buffers and controlled process failure |
| REL-08 | Shared child handles multiple requests; one times out | Other calls not killed merely by that request timeout |
| REL-09 | Gateway SIGTERM during traffic | Readiness removed, bounded drain, children reaped |
| REL-10 | Rate/session identity churn | Bounded caches, counters and memory |
| REL-11 | Optional upstream first appears in background | No unreviewed initial tool contract becomes public |
| REL-12 | Schema or description changes between refreshes | New calls quarantine the affected export; operator sees drift |
| REL-13 | Many near-limit responses arrive concurrently | Shared buffer reservations prevent aggregate exhaustion |
| REL-14 | Log sink blocks during shutdown | Bounded drain and explicit audit-delivery limitation |

### 23.4 Performance qualification

Use a declared benchmark machine, build version, payload distribution, catalog size and upstream simulator. A proposed initial qualification workload is 1,000 exported tools, 100 concurrent read calls, 1 KiB JSON requests/results, and a mock upstream with controlled 20 ms latency on a 2-vCPU/2-GiB container.

Proposed targets: p95 gateway-added processing time below 5 ms excluding queue/upstream latency; no correctness failures during a sustained 30-minute run; stable goroutine/file-descriptor counts after repeated reloads; and memory remaining within configured limits during hostile-size tests. These are acceptance targets, not benchmark results.

Run unit tests, integration tests, the Go race detector, fuzzing for parsers/serializers/cursors, and protocol conformance for every enabled era/transport combination. Record results in the release artifact. Passing a dependency’s test suite does not qualify gateway-specific authentication, aliasing or snapshot handling.

## 24. Implementation backlog and dependency order

| Work item | Depends on | Deliverable and completion gate |
| --- | --- | --- |
| IMP-01: Contracts and configuration | None | Strict schema/decoder, domain types, diagnostics; CFG-01 through CFG-04 |
| IMP-02: Compiler and runtime store | IMP-01 | Export compilation, leases, candidate transaction; reload race tests |
| IMP-03: Identity and policy foundation | IMP-01 | Resource-server metadata, verifier, credential refs, deny-by-default policies |
| IMP-04: REST vertical slice | IMP-02, IMP-03 | One real MCP-to-REST call, schema validation, envelope and audit |
| IMP-05: Modern protocol facade | IMP-04 | Full metadata/header/error handling and bounded SSE tool progress |
| IMP-06: Remote MCP adapter | IMP-02, IMP-03, IMP-05 | Discovery, aliases, pools, contract refresh and isolation |
| IMP-07: Legacy compatibility | IMP-05, IMP-06 | Lifecycle, sessions, cross-era tool subset and cancellation semantics |
| IMP-08: Stdio supervisor | IMP-06, IMP-07 | Controlled launch, framing, partitions, restart/drain lifecycle |
| IMP-09: Reliability and operations | IMP-04 through IMP-08 | Admission, safe retries, circuit state, admin endpoints and telemetry |
| IMP-10: Release hardening | All preceding | Conformance, security, fault, fuzz/race tests and documented deployment |
| IMP-11: Typed resources/prompts | After v1 qualification | End-to-end URI/policy/content behavior; extension release gate |
| IMP-12: Interactive/subscription bridge | After v1 qualification | Continuation security, capability translation and bounded streams |
| IMP-13: Delegated identity/durable features | Separate security design | Consent, token lifecycle, persistence and restart recovery |

For IMP-04, use a minimal modern facade internally to prove the vertical slice; IMP-05 completes its conformance surface. A work item is complete only when its error paths and ownership tests pass, not when the happy-path demo responds.

The first production release requires REST, remote MCP and stdio upstream support together with the declared legacy subset. A smaller REST-only milestone is useful for development but must not be marketed as the completed universal gateway.

## 25. Reference kit and verification boundary

The accompanying kit contains the implementation document, a full proposed YAML configuration, its structural JSON Schema, a sample OpenAPI operation, modern MCP request/result fixtures, and dependency-free Go interface definitions.

The reference assets can be parsed and checked without running any upstream service. The validation report states the checks actually performed. Host connectivity, real OAuth exchange, full MCP conformance, gateway security behavior and throughput remain implementation/release tests; no gateway executable is included.

The document is the behavior contract. The configuration schema is authoritative for allowed v1 syntax; the semantic compiler must enforce the additional invariants described here. When a later change affects wire compatibility, identity isolation, persistence or side-effect semantics, record a new architecture decision and update the configuration version, tests and documentation together.

## 26. Source register

Protocol and library facts were checked against these primary sources on October 19, 2026. URLs identify the reviewed sources; exact dependency versions still require locking and validation during implementation. The gateway-specific module structure, policies, defaults, algorithms, error labels and work plan are proposed design decisions.

**[S01] MCP specification, revision 2026-07-28.**

`https://modelcontextprotocol.io/specification/2026-07-28`

**[S02] MCP versioning and compatibility.**

`https://modelcontextprotocol.io/specification/2026-07-28/basic/versioning`

**[S03] MCP Streamable HTTP binding.**

`https://modelcontextprotocol.io/specification/2026-07-28/basic/transports/streamable-http`

**[S04] MCP request metadata and JSON Schema requirements.**

`https://modelcontextprotocol.io/specification/2026-07-28/basic/index`

**[S05] MCP tools.**

`https://modelcontextprotocol.io/specification/2026-07-28/server/tools`

**[S06] MCP server discovery.**

`https://modelcontextprotocol.io/specification/2026-07-28/server/discover`

**[S07] MCP caching.**

`https://modelcontextprotocol.io/specification/2026-07-28/server/utilities/caching`

**[S08] MCP subscriptions.**

`https://modelcontextprotocol.io/specification/2026-07-28/basic/patterns/subscriptions`

**[S09] MCP multi round-trip requests.**

`https://modelcontextprotocol.io/specification/2026-07-28/basic/patterns/mrtr`

**[S10] MCP cancellation.**

`https://modelcontextprotocol.io/specification/2026-07-28/basic/patterns/cancellation`

**[S11] MCP stdio binding.**

`https://modelcontextprotocol.io/specification/2026-07-28/basic/transports/stdio`

**[S12] MCP authorization.**

`https://modelcontextprotocol.io/specification/2026-07-28/basic/authorization`

**[S13] MCP security best practices.**

`https://modelcontextprotocol.io/docs/2026-07-28/tutorials/security/security_best_practices`

**[S14] Legacy MCP transport, revision 2025-11-25.**

`https://modelcontextprotocol.io/specification/2025-11-25/basic/transports`

**[S15] Legacy MCP lifecycle, revision 2025-11-25.**

`https://modelcontextprotocol.io/specification/2025-11-25/basic/lifecycle`

**[S16] Legacy MCP tools, revision 2025-11-25.**

`https://modelcontextprotocol.io/specification/2025-11-25/server/tools`

**[S17] Official MCP Go SDK and compatibility table.**

`https://github.com/modelcontextprotocol/go-sdk`

**[S18] OpenAPI Specification 3.1.1.**

`https://spec.openapis.org/oas/v3.1.1.html`

**[S19] libopenapi project.**

`https://github.com/pb33f/libopenapi`

**[S20] Go JSON Schema validator.**

`https://github.com/santhosh-tekuri/jsonschema`

**[S21] Go net/http package.**

`https://pkg.go.dev/net/http`

**[S22] Go release history and support policy.**

`https://go.dev/doc/devel/release`

**[S23] MCP changes in revision 2026-07-28.**

`https://modelcontextprotocol.io/specification/2026-07-28/changelog`

