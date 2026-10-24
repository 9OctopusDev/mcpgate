package conformance_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/9OctopusDev/mcpgate/internal/protocol/modern"
)

func TestModernHTTPDiscoveryGolden(t *testing.T) {
	handler := modern.Handler(modern.Dependencies{ServerID: "public"})
	body := `{"jsonrpc":"2.0","id":"normalized-id","method":"server/discover","params":{},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientInfo":{"name":"conformance","version":"1"},"io.modelcontextprotocol/clientCapabilities":{}}}`
	request := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	request.Header.Set("MCP-Protocol-Version", modern.ProtocolVersion)
	request.Header.Set("MCP-Client-Name", "conformance")
	request.Header.Set("MCP-Client-Version", "1")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	expected := "{\"jsonrpc\":\"2.0\",\"id\":\"normalized-id\",\"result\":{\"capabilities\":{\"tools\":{}},\"protocolVersion\":\"2026-07-28\",\"serverInfo\":{\"name\":\"mcpgate\",\"version\":\"v1\"}}}\n"
	if response.Code != 200 || response.Header().Get("Cache-Control") != "no-store" || response.Body.String() != expected {
		t.Fatalf("status=%d headers=%v\nwant=%s\ngot=%s", response.Code, response.Header(), expected, response.Body.String())
	}
}
