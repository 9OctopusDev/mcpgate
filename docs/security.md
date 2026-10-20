# Security boundaries

The gateway authenticates callers as an OAuth protected resource. Principals are created only from verified issuer, audience, signature, time, tenant, subject/client, and scope claims. Policy is deny-by-default and is rechecked immediately before dispatch against the live revocation epoch.

Downstream credentials, cookies, forwarding headers, host values, MCP session identifiers, raw JSON-RPC identifiers, arguments, and results are never forwarded upstream. Each upstream receives only its configured credential and allowlisted trace context. DNS results are checked against the network policy immediately before dialing; redirects and environment proxies are disabled while TLS verification retains the configured server name.

The gateway authorizes exported tool invocation, not object-level access inside an upstream. Service credentials can have broader authority than a caller; every export using one requires explicit operator acknowledgement and upstream-side object authorization remains mandatory.

Configuration and OpenAPI inputs are local, bounded, UTF-8, duplicate-key checked, symlink-root checked, and external references are disabled. Discovery output is untrusted: additions are not auto-published and changed/removed accepted contracts are quarantined. Tool content can never request credentials, packages, commands, configuration changes, or scopes.

Logs, diagnostics, metrics, traces, admin output, panic recovery, SDK logging, and child stderr must not contain secret values, raw payloads, session IDs, cursor material, or low-entropy identity hashes. Audit identity uses a keyed partition digest. Payload logging is unsupported in v1.

TLS is required at ingress. The loopback admin interface must not be exposed by a Service, ingress, host port, or sidecar proxy. The binary has no durable store, hosted control plane, plugin installation, or model-provider integration.

