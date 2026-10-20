package modern

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

func FuzzModernMetadataAndJSON(f *testing.F) {
	f.Add([]byte(`{"jsonrpc":"2.0","id":1,"method":"server/discover","params":{},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientInfo":{"name":"fuzz","version":"1"},"io.modelcontextprotocol/clientCapabilities":{}}}`))
	handler := Handler(Dependencies{ServerID: "fuzz"})
	f.Fuzz(func(t *testing.T, body []byte) {
		if len(body) > 1<<16 {
			return
		}
		request := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
		request.Header.Set("MCP-Protocol-Version", ProtocolVersion)
		request.Header.Set("MCP-Client-Name", "fuzz")
		request.Header.Set("MCP-Client-Version", "1")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code < 200 || response.Code > 599 {
			t.Fatalf("invalid status %d", response.Code)
		}
	})
}
