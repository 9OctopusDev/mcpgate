package integration_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	adaptermcp "github.com/9OctopusDev/mcpgate/internal/adapters/mcp"
	"github.com/9OctopusDev/mcpgate/internal/contracts"
)

type remoteMCPTransport struct {
	mu       sync.Mutex
	requests []map[string]any
}

func (transport *remoteMCPTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	body, _ := io.ReadAll(request.Body)
	var message map[string]any
	_ = json.Unmarshal(body, &message)
	transport.mu.Lock()
	transport.requests = append(transport.requests, message)
	transport.mu.Unlock()
	method := message["method"].(string)
	response := `{"jsonrpc":"2.0","id":"u","result":{}}`
	if method == "server/discover" {
		response = `{"jsonrpc":"2.0","id":"u","result":{"protocolVersion":"2026-07-28"}}`
	}
	if method == "tools/list" {
		response = `{"jsonrpc":"2.0","id":"u","result":{"tools":[{"name":"native.echo","inputSchema":{"type":"object"}}]}}`
	}
	if method == "tools/call" {
		response = `{"jsonrpc":"2.0","id":"u","result":{"content":[{"type":"text","text":"ok"}],"structuredContent":{"ok":true}}}`
	}
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(response)), Request: request}, nil
}

func TestRemoteMCPDiscoveryAliasAndContent(t *testing.T) {
	transport := &remoteMCPTransport{}
	adapter, err := adaptermcp.NewHTTPAdapter(adaptermcp.HTTPOptions{Endpoint: "https://remote.example/mcp", Client: &http.Client{Transport: transport}, Limits: adaptermcp.DefaultLimits()})
	if err != nil {
		t.Fatal(err)
	}
	discovery, err := adapter.DetectAndDiscover(context.Background())
	if err != nil || len(discovery.Catalog.Tools) != 1 {
		t.Fatalf("discovery=%+v err=%v", discovery, err)
	}
	outcome, err := adapter.CallTool(context.Background(), contracts.Call{Route: contracts.Route{Key: contracts.RouteKey{PublicName: "public.echo"}, NativeName: "native.echo"}, Arguments: json.RawMessage(`{"value":"x"}`)}, nil)
	if err != nil || outcome.Complete == nil || string(outcome.Complete.Structured) != `{"ok":true}` {
		t.Fatalf("outcome=%+v err=%v", outcome, err)
	}
	transport.mu.Lock()
	defer transport.mu.Unlock()
	last := transport.requests[len(transport.requests)-1]
	params := last["params"].(map[string]any)
	if params["name"] != "native.echo" {
		t.Fatalf("public name leaked into upstream call: %v", params)
	}
}
