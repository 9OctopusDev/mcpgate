package integration_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mcpgate/mcpgate/internal/adapters/rest"
	"github.com/mcpgate/mcpgate/internal/config"
	"github.com/mcpgate/mcpgate/internal/contracts"
	"github.com/mcpgate/mcpgate/internal/edge"
	"github.com/mcpgate/mcpgate/internal/executor"
	"github.com/mcpgate/mcpgate/internal/policy"
	"github.com/mcpgate/mcpgate/internal/protocol/modern"
	"github.com/mcpgate/mcpgate/internal/testkit"
)

type integrationVerifier struct{}

func (integrationVerifier) Verify(context.Context, string, string) (contracts.Principal, error) {
	return contracts.Principal{Subject: "user", PartitionDigest: "digest"}, nil
}

func TestModernRESTVerticalSlice(t *testing.T) {
	document := []byte(`openapi: 3.1.0
info: {title: Users, version: v1}
paths:
  /users/{id}:
    get:
      operationId: getUser
      parameters:
        - {name: id, in: path, required: true, schema: {type: string}}
      responses:
        "200":
          description: ok
          content: {application/json: {schema: {type: object, required: [id], properties: {id: {type: string}}}}}
`)
	plan, err := rest.Compile(document, rest.CompileOptions{OperationID: "getUser", EndpointBase: "https://api.example/v1"})
	if err != nil {
		t.Fatal(err)
	}
	recording := &testkit.RecordingHTTP{Handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"id":"u1"}`))
	})}
	adapter, err := rest.NewAdapter(map[string]*rest.RESTPlan{"getUser": plan}, &http.Client{Transport: recording}, nil, rest.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	authorizer, err := policy.Compile(map[string]config.Policy{"allow": {Subjects: []string{"user"}}})
	if err != nil {
		t.Fatal(err)
	}
	route := contracts.Route{Key: contracts.RouteKey{ServerID: "public", PublicName: "users.get"}, UpstreamID: "users", NativeName: "getUser", PolicyID: "allow", Behavior: contracts.BehaviorReadOnly, MaxAttempts: 1}
	execution, err := executor.New(executor.Dependencies{Authorizer: authorizer, Adapters: executor.StaticAdapters{"users": adapter}}, executor.Limits{Active: 4, Queued: 4, QueueWait: time.Second, BufferBytes: 1 << 20, PerPrincipalRate: 100, PerPrincipalBurst: 10})
	if err != nil {
		t.Fatal(err)
	}
	modernHandler := modern.Handler(modern.Dependencies{ServerID: "public", Resolve: func(name string) (contracts.Route, bool) { return route, name == "users.get" }, Execute: execution})
	handler, err := edge.New([]edge.Route{{ServerID: "public", Host: "mcp.example", Path: "/mcp", ResourceURI: "https://mcp.example/mcp", Handler: modernHandler}}, integrationVerifier{}, edge.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	body := `{"jsonrpc":"2.0","id":"r1","method":"tools/call","params":{"name":"users.get","arguments":{"path":{"id":"u1"}}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientInfo":{"name":"agent","version":"1"},"io.modelcontextprotocol/clientCapabilities":{}}}`
	request := httptest.NewRequest(http.MethodPost, "https://mcp.example/mcp", strings.NewReader(body))
	request.Host = "mcp.example"
	request.Header.Set("Authorization", "Bearer downstream")
	request.Header.Set("MCP-Protocol-Version", "2026-07-28")
	request.Header.Set("MCP-Client-Name", "agent")
	request.Header.Set("MCP-Client-Version", "1")
	request.Header.Set("MCP-Name", "users.get")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"id":"u1"`) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if got := recording.Requests(); len(got) != 1 || got[0].URL != "https://api.example/v1/users/u1" || got[0].Header.Get("Authorization") != "" {
		t.Fatalf("upstream=%+v", got)
	}
}
