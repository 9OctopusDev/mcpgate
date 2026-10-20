package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mcpgate/mcpgate/internal/contracts"
)

type scriptedTransport struct {
	mu      sync.Mutex
	methods []string
	handler func(string, map[string]any) string
}

func (transport *scriptedTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	body, _ := io.ReadAll(request.Body)
	var message map[string]any
	_ = json.Unmarshal(body, &message)
	method, _ := message["method"].(string)
	transport.mu.Lock()
	transport.methods = append(transport.methods, method)
	transport.mu.Unlock()
	response := transport.handler(method, message)
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(response)), Request: request}, nil
}

func TestDetectAndDiscoverModernWithoutExecutingTools(t *testing.T) {
	transport := &scriptedTransport{handler: func(method string, message map[string]any) string {
		id, _ := json.Marshal(message["id"])
		switch method {
		case "server/discover":
			return `{"jsonrpc":"2.0","id":` + string(id) + `,"result":{"protocolVersion":"2026-07-28"}}`
		case "tools/list":
			return `{"jsonrpc":"2.0","id":` + string(id) + `,"result":{"tools":[{"name":"echo","description":"Echo","inputSchema":{"type":"object"}}]}}`
		default:
			t.Fatalf("unexpected method %q", method)
			return ""
		}
	}}
	adapter, err := NewHTTPAdapter(HTTPOptions{Endpoint: "https://upstream.example/mcp", Client: &http.Client{Transport: transport}, Limits: DefaultLimits()})
	if err != nil {
		t.Fatal(err)
	}
	result, err := adapter.DetectAndDiscover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Version != ModernVersion || len(result.Catalog.Tools) != 1 || result.Catalog.Tools[0].Name != "echo" {
		t.Fatalf("result=%+v", result)
	}
	for _, method := range transport.methods {
		if method == "tools/call" {
			t.Fatal("detection executed a tool")
		}
	}
}

func TestFallbackOnlyForMethodNotFoundAndRepeatedCursorRejected(t *testing.T) {
	for _, test := range []struct {
		name       string
		discover   string
		wantLegacy bool
	}{
		{"method-not-found", `{"jsonrpc":"2.0","id":"x","error":{"code":-32601,"message":"method not found"}}`, true},
		{"authentication", `{"jsonrpc":"2.0","id":"x","error":{"code":-32001,"message":"unauthorized"}}`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			transport := &scriptedTransport{handler: func(method string, _ map[string]any) string {
				switch method {
				case "server/discover":
					return test.discover
				case "initialize":
					return `{"jsonrpc":"2.0","id":"i","result":{"protocolVersion":"2025-11-25"}}`
				case "notifications/initialized":
					return `{"jsonrpc":"2.0","result":{}}`
				case "tools/list":
					return `{"jsonrpc":"2.0","id":"l","result":{"tools":[],"nextCursor":"same"}}`
				default:
					return `{"jsonrpc":"2.0","result":{}}`
				}
			}}
			adapter, _ := NewHTTPAdapter(HTTPOptions{Endpoint: "https://upstream.example/mcp", Client: &http.Client{Transport: transport}, Limits: Limits{MaxPages: 3, MaxTools: 10, MaxBytes: 1 << 20, DiscoveryTimeout: time.Second}})
			_, err := adapter.DetectAndDiscover(context.Background())
			initialized := false
			for _, method := range transport.methods {
				initialized = initialized || method == "initialize"
			}
			if initialized != test.wantLegacy {
				t.Fatalf("initialized=%v err=%v", initialized, err)
			}
			if err == nil {
				t.Fatal("expected discovery failure")
			}
		})
	}
}

func TestCallPreservesNativeContentAndRebuildsMetadata(t *testing.T) {
	transport := &scriptedTransport{handler: func(method string, message map[string]any) string {
		if method != "tools/call" {
			t.Fatalf("method=%s", method)
		}
		params := message["params"].(map[string]any)
		if params["name"] != "native.echo" {
			t.Fatalf("params=%v", params)
		}
		return `{"jsonrpc":"2.0","id":"upstream","result":{"content":[{"type":"text","text":"failed safely"}],"structuredContent":{"code":"E"},"isError":true,"_meta":{"safe":"yes"}}}`
	}}
	adapter, _ := NewHTTPAdapter(HTTPOptions{Endpoint: "https://upstream.example/mcp", Client: &http.Client{Transport: transport}, Limits: DefaultLimits(), Version: ModernVersion})
	outcome, err := adapter.CallTool(context.Background(), contracts.Call{ExecutionID: "e", RequestID: json.RawMessage(`"downstream"`), Route: contracts.Route{NativeName: "native.echo"}, Arguments: json.RawMessage(`{"value":"x"}`)}, nil)
	if err != nil || outcome.Complete == nil || !outcome.Complete.IsError || len(outcome.Complete.Content) != 1 {
		t.Fatalf("outcome=%+v err=%v", outcome, err)
	}
}
