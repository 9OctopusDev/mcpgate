package benchmarks_test

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mcpgate/mcpgate/internal/contracts"
	"github.com/mcpgate/mcpgate/internal/protocol/modern"
)

func BenchmarkGateway(b *testing.B) {
	tools := make([]contracts.ToolContract, 1000)
	for index := range tools {
		tools[index] = contracts.ToolContract{Name: fmt.Sprintf("tool.%04d", index), InputSchema: []byte(`{"type":"object"}`)}
	}
	handler := modern.Handler(modern.Dependencies{ServerID: "benchmark", Principal: func(*http.Request) (contracts.Principal, bool) {
		return contracts.Principal{PartitionDigest: "benchmark"}, true
	}, List: func(context.Context, contracts.Principal, string) (modern.CatalogPage, error) {
		return modern.CatalogPage{Tools: tools, CacheScope: "private"}, nil
	}})
	body := []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientInfo":{"name":"bench","version":"1"},"io.modelcontextprotocol/clientCapabilities":{}}}`)
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			request := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
			request.Header.Set("MCP-Protocol-Version", modern.ProtocolVersion)
			request.Header.Set("MCP-Client-Name", "bench")
			request.Header.Set("MCP-Client-Version", "1")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != 200 {
				b.Fatalf("status=%d", response.Code)
			}
		}
	})
}
