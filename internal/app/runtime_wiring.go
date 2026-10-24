package app

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/9OctopusDev/mcpgate/internal/adapters/mcp"
	"github.com/9OctopusDev/mcpgate/internal/adapters/rest"
	"github.com/9OctopusDev/mcpgate/internal/admin"
	"github.com/9OctopusDev/mcpgate/internal/catalog"
	"github.com/9OctopusDev/mcpgate/internal/compiler"
	"github.com/9OctopusDev/mcpgate/internal/config"
	"github.com/9OctopusDev/mcpgate/internal/contracts"
	"github.com/9OctopusDev/mcpgate/internal/credentials"
	"github.com/9OctopusDev/mcpgate/internal/edge"
	"github.com/9OctopusDev/mcpgate/internal/executor"
	"github.com/9OctopusDev/mcpgate/internal/identity"
	"github.com/9OctopusDev/mcpgate/internal/policy"
	"github.com/9OctopusDev/mcpgate/internal/protocol/legacy"
	"github.com/9OctopusDev/mcpgate/internal/protocol/modern"
	gatewayruntime "github.com/9OctopusDev/mcpgate/internal/runtime"
	"github.com/9OctopusDev/mcpgate/internal/supervisor"
	"github.com/9OctopusDev/mcpgate/internal/telemetry"
	"github.com/9OctopusDev/mcpgate/internal/upstream"
	"github.com/goccy/go-yaml"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type handlerBox struct{ handler http.Handler }

type dynamicHandler struct{ current atomic.Pointer[handlerBox] }

func (handler *dynamicHandler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	box := handler.current.Load()
	if box == nil || box.handler == nil {
		http.Error(writer, "gateway not ready", http.StatusServiceUnavailable)
		return
	}
	box.handler.ServeHTTP(writer, request)
}

func (handler *dynamicHandler) Store(next http.Handler) {
	handler.current.Store(&handlerBox{handler: next})
}

type verifierSet struct{ byResource map[string]*identity.Verifier }

func (set verifierSet) Verify(ctx context.Context, token, resource string) (contracts.Principal, error) {
	verifier := set.byResource[resource]
	if verifier == nil {
		return contracts.Principal{}, errors.New("identity verifier unavailable")
	}
	return verifier.Verify(ctx, token, resource)
}

type runtimeBundle struct {
	document  *config.Document
	handler   http.Handler
	routes    []map[string]string
	upstreams []map[string]string
}

type runtimeBuilder struct {
	path         string
	partitionKey []byte
	mu           sync.Mutex
	pending      *runtimeBundle
}

func (builder *runtimeBuilder) Build(ctx context.Context) (gatewayruntime.Candidate, error) {
	bundle, generation, err := builder.build(ctx)
	if err != nil {
		return gatewayruntime.Candidate{}, err
	}
	builder.mu.Lock()
	builder.pending = bundle
	builder.mu.Unlock()
	topology := bundle.document.Gateway.Data.Listen + "\x00" + bundle.document.Gateway.Admin.Listen
	return gatewayruntime.Candidate{Generation: generation, ListenerTopology: topology}, nil
}

func (builder *runtimeBuilder) takePending() *runtimeBundle {
	builder.mu.Lock()
	defer builder.mu.Unlock()
	bundle := builder.pending
	builder.pending = nil
	return bundle
}

func (builder *runtimeBuilder) build(ctx context.Context) (*runtimeBundle, *gatewayruntime.Generation, error) {
	document, diagnostics := loadForCLI(ctx, builder.path)
	if document == nil || len(diagnostics) != 0 {
		return nil, nil, diagnosticError(diagnostics)
	}
	authorizer, err := policy.Compile(document.Policies)
	if err != nil {
		return nil, nil, err
	}

	bindings := make(map[string]contracts.CredentialBinding)
	cursorKeys := make(map[string]contracts.CredentialBinding)
	identities := make(map[string]contracts.CredentialIdentity)
	adapters := make(executor.StaticAdapters)
	manifests := make(map[string]contracts.Catalog)
	securityResolved := make(map[string]bool)
	var supervisors []*supervisor.Supervisor
	cleanup := func(closeCtx context.Context) error {
		var first error
		for _, adapter := range adapters {
			if closeErr := adapter.Close(closeCtx); closeErr != nil && first == nil {
				first = closeErr
			}
		}
		for _, processSupervisor := range supervisors {
			if closeErr := processSupervisor.Close(closeCtx); closeErr != nil && first == nil {
				first = closeErr
			}
		}
		for _, binding := range bindings {
			binding.Close()
		}
		for _, binding := range cursorKeys {
			binding.Close()
		}
		return first
	}
	fail := func(cause error) (*runtimeBundle, *gatewayruntime.Generation, error) {
		_ = cleanup(context.Background())
		return nil, nil, cause
	}

	for serverID, server := range document.Servers {
		key, resolveErr := credentials.ResolveCursorKey(ctx, document.Secrets[server.Catalog.CursorSecret])
		if resolveErr != nil {
			return fail(fmt.Errorf("server %s cursor credential: %w", serverID, resolveErr))
		}
		cursorKeys[serverID] = key
		identities[server.Catalog.CursorSecret] = key.Identity()
	}
	for upstreamID, declaration := range document.Upstreams {
		if declaration.Auth.Secret == "" {
			continue
		}
		binding, resolveErr := credentials.Resolve(ctx, document.Secrets[declaration.Auth.Secret])
		if resolveErr != nil {
			if declaration.Required {
				return fail(fmt.Errorf("upstream %s credential: %w", upstreamID, resolveErr))
			}
			continue
		}
		bindings[upstreamID] = binding
		identities[declaration.Auth.Secret] = binding.Identity()
	}

	base := filepath.Dir(document.Source.Resolved)
	sourceRoot := filepath.Dir(base)
	for upstreamID, declaration := range document.Upstreams {
		adapter, manifest, prepareErr := prepareAdapter(ctx, document, base, sourceRoot, upstreamID, declaration, bindings[upstreamID], &supervisors)
		if prepareErr != nil {
			if declaration.Required {
				return fail(fmt.Errorf("required upstream %s: %w", upstreamID, prepareErr))
			}
			continue
		}
		adapters[upstreamID] = adapter
		manifests[upstreamID] = manifest
		if declaration.Kind == "rest" {
			securityResolved[upstreamID] = true
		}
	}

	resource := gatewayruntime.NewSharedResource(cleanup)
	generation, compileDiagnostics := compiler.Compile(ctx, compiler.Inputs{
		Document: document, CredentialIdentities: identities, Manifests: manifests,
		SecurityResolved: securityResolved, Resources: []*gatewayruntime.SharedResource{resource},
	})
	if len(compileDiagnostics) != 0 {
		return fail(diagnosticError(compileDiagnostics))
	}

	verifiers := make(map[string]*identity.Verifier, len(document.Authentication))
	for name, authentication := range document.Authentication {
		parsed, parseErr := url.Parse(authentication.JWKSURI)
		if parseErr != nil || parsed.Scheme != "https" || parsed.Hostname() == "" {
			return fail(fmt.Errorf("authentication %s has invalid JWKS URI", name))
		}
		transport := upstream.NewTransport(upstream.NetworkPolicy{ServerName: parsed.Hostname(), AllowedHosts: []string{parsed.Hostname()}, PublicOnly: true})
		if transport == nil {
			return fail(fmt.Errorf("authentication %s transport is invalid", name))
		}
		verifier, verifierErr := identity.NewVerifier(authentication, upstream.NewClient(transport), builder.partitionKey)
		if verifierErr != nil {
			return fail(fmt.Errorf("authentication %s: %w", name, verifierErr))
		}
		verifiers[name] = verifier
	}

	execution, err := executor.New(executor.Dependencies{Authorizer: authorizer, Adapters: adapters}, executor.Limits{
		Active: document.Gateway.Limits.MaxInflight, Queued: document.Gateway.Limits.MaxQueued,
		QueueWait: document.Gateway.Limits.QueueTimeout.Duration, BufferBytes: document.Gateway.Limits.BufferBudgetBytes,
		PerPrincipalRate: float64(document.Gateway.Limits.PerPrincipalRPS), PerPrincipalBurst: document.Gateway.Limits.PerPrincipalBurst,
	})
	if err != nil {
		return fail(err)
	}
	sessions := legacy.NewSessions(document.Gateway.Lifecycle.MaxLegacySessions, document.Gateway.Lifecycle.LegacySessionIdle.Duration, nil)
	edgeRoutes := make([]edge.Route, 0, len(document.Servers)*len(document.Gateway.Data.AllowedHosts))
	var routeSummaries []map[string]string
	serverIDs := sortedStringKeys(document.Servers)
	for _, serverID := range serverIDs {
		serverConfig := document.Servers[serverID]
		virtual, _ := generation.Server(serverID)
		routes := make([]contracts.Route, 0, len(virtual.RouteKeys))
		for _, key := range virtual.RouteKeys {
			route, _ := generation.Route(key)
			routes = append(routes, route)
			routeSummaries = append(routeSummaries, map[string]string{"server": serverID, "tool": key.PublicName, "upstream": route.UpstreamID, "policy": route.PolicyID})
		}
		resolve := func(name string) (contracts.Route, bool) {
			return generation.Route(contracts.RouteKey{ServerID: serverID, PublicName: name})
		}
		listPage := func(listCtx context.Context, principal contracts.Principal, cursor string, pageSize int) (catalog.ListPage, error) {
			return catalog.List(listCtx, catalog.ListRequest{
				ServerID: serverID, Principal: principal, Routes: routes, Authorizer: authorizer,
				Revision: generation.Revision(), PageSize: pageSize, Cursor: cursor, CursorKey: cursorKeys[serverID],
				CursorTTL:         time.Duration(serverConfig.Catalog.TTLMillis) * time.Millisecond,
				SupportedFeatures: map[string]bool{"tools": true}, Available: func(route contracts.Route) bool { _, ok := adapters[route.UpstreamID]; return ok },
			})
		}
		modernHandler := modern.Handler(modern.Dependencies{
			ServerID: serverID, Revision: generation.Revision(), Resolve: resolve, Execute: execution, Cancel: execution.Cancel,
			List: func(listCtx context.Context, principal contracts.Principal, cursor string) (modern.CatalogPage, error) {
				page, listErr := listPage(listCtx, principal, cursor, serverConfig.Catalog.PageSize)
				return modern.CatalogPage{Tools: page.Tools, NextCursor: page.NextCursor, TTLMillis: page.TTLMillis, CacheScope: page.CacheScope}, listErr
			},
		})
		legacyHandler := legacy.Handler(legacy.Dependencies{
			ServerID: serverID, Sessions: sessions, Resolve: resolve, Execute: execution, Cancel: execution.Cancel,
			CallTimeout: document.Gateway.Limits.CallTimeout.Duration,
			List: func(listCtx context.Context, principal contracts.Principal) ([]contracts.ToolContract, error) {
				page, listErr := listPage(listCtx, principal, "", len(routes)+1)
				return page.Tools, listErr
			},
		})
		protocolHandler := protocolMux(serverConfig.Protocols, modernHandler, legacyHandler)
		for _, host := range document.Gateway.Data.AllowedHosts {
			edgeRoutes = append(edgeRoutes, edge.Route{
				ServerID: serverID, Host: host, Path: serverConfig.Path, ResourceURI: serverConfig.ResourceURI,
				Origins: append([]string(nil), document.Gateway.Data.AllowedOrigins...), RequireOrigin: !document.Gateway.Data.AllowMissingOrigin,
				Handler: protocolHandler,
			})
		}
	}
	edgeHandler, err := edge.New(edgeRoutes, verifierSet{byResource: mapServerVerifiers(document, verifiers)}, edge.Limits{
		HeaderBytes: 32 << 10, BodyBytes: document.Gateway.Limits.RequestBytes, JSONDepth: 64, ParseTimeout: document.Gateway.Limits.QueueTimeout.Duration,
	})
	if err != nil {
		return fail(err)
	}
	upstreamSummaries := make([]map[string]string, 0, len(document.Upstreams))
	for _, id := range sortedStringKeys(document.Upstreams) {
		state := "dormant"
		if _, ok := adapters[id]; ok {
			state = "ready"
		}
		upstreamSummaries = append(upstreamSummaries, map[string]string{"id": id, "kind": document.Upstreams[id].Kind, "state": state})
	}
	return &runtimeBundle{document: document, handler: edgeHandler, routes: routeSummaries, upstreams: upstreamSummaries}, generation, nil
}

func diagnosticError(diagnostics []config.Diagnostic) error {
	if len(diagnostics) == 0 {
		return errors.New("configuration is unavailable")
	}
	return fmt.Errorf("%s: %s", diagnostics[0].Code, diagnostics[0].Message)
}

func prepareAdapter(ctx context.Context, document *config.Document, base, sourceRoot, id string, declaration config.Upstream, binding contracts.CredentialBinding, supervisors *[]*supervisor.Supervisor) (contracts.ToolAdapter, contracts.Catalog, error) {
	switch declaration.Kind {
	case "rest":
		content, _, err := config.ReadLocal(base, declaration.OpenAPI.File, []string{base, sourceRoot}, document.Gateway.Limits.SchemaBytes)
		if err != nil {
			return nil, contracts.Catalog{}, err
		}
		bindings := openAPISecurityBindings(content, declaration.Auth.Secret != "")
		plans := make(map[string]*rest.RESTPlan)
		for _, server := range document.Servers {
			for _, export := range server.Exports {
				if export.Upstream != id {
					continue
				}
				if _, exists := plans[export.Native]; exists {
					continue
				}
				plan, compileErr := rest.Compile(content, rest.CompileOptions{OperationID: export.Native, EndpointBase: declaration.Endpoints[0].URL, SecurityBindings: bindings, MaxDocumentBytes: int(document.Gateway.Limits.SchemaBytes)})
				if compileErr != nil {
					return nil, contracts.Catalog{}, compileErr
				}
				plans[export.Native] = plan
			}
		}
		client, err := upstreamClient(declaration)
		if err != nil {
			return nil, contracts.Catalog{}, err
		}
		adapter, err := rest.NewAdapter(plans, client, binding, rest.Limits{RequestBytes: document.Gateway.Limits.RequestBytes, EncodedResponseBytes: document.Gateway.Limits.ResponseBytes, ResponseBytes: document.Gateway.Limits.ResponseBytes, OutputBytes: document.Gateway.Limits.ResponseBytes})
		if err != nil {
			return nil, contracts.Catalog{}, err
		}
		manifest, err := adapter.Discover(ctx, binding)
		return adapter, manifest, err
	case "mcp_http":
		client, err := upstreamClient(declaration)
		if err != nil {
			return nil, contracts.Catalog{}, err
		}
		adapter, err := mcp.NewHTTPAdapter(mcp.HTTPOptions{Endpoint: declaration.Endpoints[0].URL, Client: client, Credential: binding, Limits: mcp.DefaultLimits()})
		if err != nil {
			return nil, contracts.Catalog{}, err
		}
		manifest, err := adapter.Discover(ctx, binding)
		return adapter, manifest, err
	case "mcp_stdio":
		return prepareStdioAdapter(ctx, declaration, supervisors)
	default:
		return nil, contracts.Catalog{}, fmt.Errorf("unsupported upstream kind %q", declaration.Kind)
	}
}

func prepareStdioAdapter(ctx context.Context, declaration config.Upstream, supervisors *[]*supervisor.Supervisor) (contracts.ToolAdapter, contracts.Catalog, error) {
	processSupervisor, err := supervisor.New(supervisor.Config{
		Executable: declaration.Process.Executable, Args: append([]string(nil), declaration.Process.Args...), CWD: declaration.Process.CWD,
		Env: cloneStrings(declaration.Process.Env), MaxProcesses: declaration.Process.MaxProcesses, MaxInflight: declaration.Execution.MaxInflight,
		MaxFrameBytes: int(declaration.Process.MaxFrameBytes), StartupAttempts: 3, Grace: 2 * time.Second,
	})
	if err != nil {
		return nil, contracts.Catalog{}, err
	}
	lease, err := processSupervisor.Acquire(ctx, supervisor.PartitionKey{Generation: "startup"})
	if err != nil {
		_ = processSupervisor.Close(context.Background())
		return nil, contracts.Catalog{}, err
	}
	manifest, err := discoverStdio(ctx, lease)
	if err != nil {
		lease.Release()
		_ = processSupervisor.Close(context.Background())
		return nil, contracts.Catalog{}, err
	}
	adapter, err := mcp.NewStdioClient(lease, manifest, lease.Release)
	if err != nil {
		lease.Release()
		_ = processSupervisor.Close(context.Background())
		return nil, contracts.Catalog{}, err
	}
	*supervisors = append(*supervisors, processSupervisor)
	return adapter, manifest, nil
}

func discoverStdio(ctx context.Context, lease *supervisor.Lease) (contracts.Catalog, error) {
	request := []byte(`{"jsonrpc":"2.0","id":"discover","method":"server/discover","params":{"clientInfo":{"name":"mcpgate","version":"v1"},"capabilities":{}}}`)
	frame, err := lease.Call(ctx, json.RawMessage(`"discover"`), request)
	if err != nil {
		return contracts.Catalog{}, err
	}
	var response struct {
		Error *struct {
			Code int `json:"code"`
		} `json:"error"`
	}
	if json.Unmarshal(frame, &response) != nil || response.Error != nil {
		return contracts.Catalog{}, errors.New("stdio upstream does not support modern discovery")
	}
	listRequest := []byte(`{"jsonrpc":"2.0","id":"tools","method":"tools/list","params":{}}`)
	frame, err = lease.Call(ctx, json.RawMessage(`"tools"`), listRequest)
	if err != nil {
		return contracts.Catalog{}, err
	}
	var listed struct {
		Result struct {
			Tools []struct {
				Name, Description         string
				InputSchema, OutputSchema json.RawMessage
			} `json:"tools"`
		} `json:"result"`
		Error json.RawMessage `json:"error"`
	}
	if json.Unmarshal(frame, &listed) != nil || len(listed.Error) != 0 {
		return contracts.Catalog{}, errors.New("invalid stdio tools/list response")
	}
	manifest := contracts.Catalog{Protocol: mcp.ModernVersion}
	for _, tool := range listed.Result.Tools {
		if tool.Name == "" || !json.Valid(tool.InputSchema) {
			return contracts.Catalog{}, errors.New("invalid stdio tool contract")
		}
		contract := contracts.ToolContract{Name: tool.Name, Description: tool.Description, InputSchema: append(json.RawMessage(nil), tool.InputSchema...), OutputSchema: append(json.RawMessage(nil), tool.OutputSchema...)}
		contract.Fingerprint = catalog.Fingerprint(contract)
		manifest.Tools = append(manifest.Tools, contract)
	}
	sort.Slice(manifest.Tools, func(i, j int) bool { return manifest.Tools[i].Name < manifest.Tools[j].Name })
	return manifest, nil
}

func upstreamClient(declaration config.Upstream) (*http.Client, error) {
	if len(declaration.Endpoints) == 0 {
		return nil, errors.New("upstream has no endpoint")
	}
	parsed, err := url.Parse(declaration.Endpoints[0].URL)
	if err != nil || parsed.Hostname() == "" {
		return nil, errors.New("upstream endpoint is invalid")
	}
	policy := upstream.NetworkPolicy{ServerName: parsed.Hostname(), AllowedHosts: append([]string(nil), declaration.Network.AllowedHosts...)}
	switch values := declaration.Network.AllowedCIDRs.(type) {
	case string:
		policy.PublicOnly = values == "public_only"
	case []string:
		for _, value := range values {
			prefix, parseErr := netip.ParsePrefix(value)
			if parseErr != nil {
				return nil, parseErr
			}
			policy.AllowedCIDRs = append(policy.AllowedCIDRs, prefix)
		}
	case []any:
		for _, raw := range values {
			value, ok := raw.(string)
			if !ok {
				return nil, errors.New("upstream CIDR is invalid")
			}
			prefix, parseErr := netip.ParsePrefix(value)
			if parseErr != nil {
				return nil, parseErr
			}
			policy.AllowedCIDRs = append(policy.AllowedCIDRs, prefix)
		}
	}
	transport := upstream.NewTransport(policy)
	if transport == nil {
		return nil, errors.New("upstream network policy is invalid")
	}
	return upstream.NewClient(transport), nil
}

func openAPISecurityBindings(document []byte, configured bool) map[string]bool {
	bindings := make(map[string]bool)
	if !configured {
		return bindings
	}
	var parsed struct {
		Components struct {
			SecuritySchemes map[string]any `yaml:"securitySchemes"`
		} `yaml:"components"`
	}
	if yaml.Unmarshal(document, &parsed) == nil {
		for name := range parsed.Components.SecuritySchemes {
			bindings[name] = true
		}
	}
	return bindings
}

func protocolMux(protocols []string, modernHandler, legacyHandler http.Handler) http.Handler {
	allowed := make(map[string]bool, len(protocols))
	for _, version := range protocols {
		allowed[version] = true
	}
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		version := request.Header.Get("MCP-Protocol-Version")
		if version == legacy.ProtocolVersion && allowed[legacy.ProtocolVersion] {
			legacyHandler.ServeHTTP(writer, request)
			return
		}
		if version == modern.ProtocolVersion && allowed[modern.ProtocolVersion] {
			modernHandler.ServeHTTP(writer, request)
			return
		}
		http.Error(writer, "unsupported MCP protocol version", http.StatusBadRequest)
	})
}

func mapServerVerifiers(document *config.Document, verifiers map[string]*identity.Verifier) map[string]*identity.Verifier {
	result := make(map[string]*identity.Verifier, len(document.Servers))
	for _, server := range document.Servers {
		result[server.ResourceURI] = verifiers[server.Authentication]
	}
	return result
}

func cloneStrings(source map[string]string) map[string]string {
	result := make(map[string]string, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func sortedStringKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

type configuredRuntime struct {
	builder       *runtimeBuilder
	coordinator   *gatewayruntime.Coordinator
	dataHandler   dynamicHandler
	active        atomic.Pointer[runtimeBundle]
	listenerMu    sync.Mutex
	dataListener  net.Listener
	adminListener net.Listener
	dataServer    *http.Server
	adminServer   *http.Server
}

func newConfiguredRuntime(path string) (*configuredRuntime, error) {
	partitionKey := make([]byte, 32)
	if _, err := rand.Read(partitionKey); err != nil {
		return nil, errors.New("create identity partition key")
	}
	runtime := &configuredRuntime{builder: &runtimeBuilder{path: path, partitionKey: partitionKey}}
	store := gatewayruntime.NewStore(nil)
	coordinator, err := gatewayruntime.NewCoordinator(gatewayruntime.CoordinatorOptions{
		Store: store, Builder: runtime.builder, Activate: runtime.activate,
		CloseResources: runtime.closeListeners, ShutdownTimeout: 45 * time.Second,
	})
	if err != nil {
		return nil, err
	}
	runtime.coordinator = coordinator
	return runtime, nil
}

func (runtime *configuredRuntime) activate(_ context.Context, _ gatewayruntime.Candidate) error {
	bundle := runtime.builder.takePending()
	if bundle == nil {
		return errors.New("prepared runtime bundle is unavailable")
	}
	runtime.listenerMu.Lock()
	defer runtime.listenerMu.Unlock()
	if runtime.dataListener == nil {
		dataListener, err := net.Listen("tcp", bundle.document.Gateway.Data.Listen)
		if err != nil {
			return fmt.Errorf("bind data listener: %w", err)
		}
		adminListener, err := net.Listen("tcp", bundle.document.Gateway.Admin.Listen)
		if err != nil {
			_ = dataListener.Close()
			return fmt.Errorf("bind admin listener: %w", err)
		}
		registry := prometheus.NewRegistry()
		_, err = telemetry.NewMetrics(registry, telemetry.LabelBounds{Servers: max(1, len(bundle.document.Servers)), Tools: max(1, len(bundle.routes)), Upstreams: max(1, len(bundle.document.Upstreams))})
		if err != nil {
			_ = dataListener.Close()
			_ = adminListener.Close()
			return err
		}
		adminHandler, err := admin.New(bundle.document.Gateway.Admin.Listen, runtime, promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))
		if err != nil {
			_ = dataListener.Close()
			_ = adminListener.Close()
			return err
		}
		runtime.dataListener = dataListener
		runtime.adminListener = adminListener
		runtime.dataServer = &http.Server{Handler: &runtime.dataHandler, ReadHeaderTimeout: 5 * time.Second, MaxHeaderBytes: 32 << 10}
		runtime.adminServer = &http.Server{Handler: adminHandler, ReadHeaderTimeout: 5 * time.Second, MaxHeaderBytes: 16 << 10}
		go func() { _ = runtime.dataServer.Serve(dataListener) }()
		go func() { _ = runtime.adminServer.Serve(adminListener) }()
	}
	runtime.dataHandler.Store(bundle.handler)
	runtime.active.Store(bundle)
	return nil
}

func (runtime *configuredRuntime) closeListeners(ctx context.Context) error {
	runtime.listenerMu.Lock()
	dataServer, adminServer := runtime.dataServer, runtime.adminServer
	runtime.listenerMu.Unlock()
	var first error
	if dataServer != nil {
		if err := dataServer.Shutdown(ctx); err != nil {
			first = err
		}
	}
	if adminServer != nil {
		if err := adminServer.Shutdown(ctx); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func (runtime *configuredRuntime) Status() gatewayruntime.Status { return runtime.coordinator.Status() }

func (runtime *configuredRuntime) Upstreams() any {
	bundle := runtime.active.Load()
	if bundle == nil {
		return []map[string]string{}
	}
	return bundle.upstreams
}

func (runtime *configuredRuntime) Routes() any {
	bundle := runtime.active.Load()
	if bundle == nil {
		return []map[string]string{}
	}
	return bundle.routes
}

func max(left, right int) int {
	if left > right {
		return left
	}
	return right
}
