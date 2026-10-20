package modern

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mcpgate/mcpgate/internal/contracts"
	"github.com/mcpgate/mcpgate/internal/executor"
)

type executeFunc func(context.Context, executor.Request, contracts.EventSink) (contracts.Outcome, error)

func (fn executeFunc) Execute(ctx context.Context, request executor.Request, sink contracts.EventSink) (contracts.Outcome, error) {
	return fn(ctx, request, sink)
}

func metadataHeaders(request *http.Request) {
	request.Header.Set("MCP-Protocol-Version", ProtocolVersion)
	request.Header.Set("MCP-Client-Name", "tester")
	request.Header.Set("MCP-Client-Version", "1")
}

func TestModernCallValidatesMirrorsAndReturnsStructuredResult(t *testing.T) {
	route := contracts.Route{Key: contracts.RouteKey{ServerID: "s", PublicName: "users.get"}, NativeName: "getUser"}
	handler := Handler(Dependencies{ServerID: "s", Principal: func(*http.Request) (contracts.Principal, bool) {
		return contracts.Principal{PartitionDigest: "p"}, true
	}, Resolve: func(name string) (contracts.Route, bool) { return route, name == "users.get" }, Execute: executeFunc(func(_ context.Context, request executor.Request, _ contracts.EventSink) (contracts.Outcome, error) {
		if request.Route.NativeName != "getUser" {
			t.Fatal("wrong route")
		}
		return contracts.Outcome{Complete: contracts.NewToolResult(json.RawMessage(`{"ok":true}`), []contracts.ContentBlock{{Type: "text", Text: "ok"}}, false)}, nil
	})})
	body := `{"jsonrpc":"2.0","id":"1","method":"tools/call","params":{"name":"users.get","arguments":{"id":"1"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientInfo":{"name":"tester","version":"1"},"io.modelcontextprotocol/clientCapabilities":{}}}`
	request := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	metadataHeaders(request)
	request.Header.Set("MCP-Name", "users.get")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"structuredContent":{"ok":true}`) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	metadataHeaders(request)
	request.Header.Set("MCP-Name", "users.get")
	request.Header.Set("MCP-Client-Name", "mismatch")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code < 400 {
		t.Fatalf("mismatched metadata accepted: %s", response.Body.String())
	}
}

func TestModernRejectsUnsupportedCapability(t *testing.T) {
	handler := Handler(Dependencies{ServerID: "s", Principal: func(*http.Request) (contracts.Principal, bool) {
		return contracts.Principal{PartitionDigest: "p"}, true
	}})
	body := `{"jsonrpc":"2.0","id":1,"method":"resources/list","params":{},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientInfo":{"name":"tester","version":"1"},"io.modelcontextprotocol/clientCapabilities":{}}}`
	request := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	metadataHeaders(request)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound || !strings.Contains(response.Body.String(), `-32601`) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}
