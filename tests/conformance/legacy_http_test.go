package conformance_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mcpgate/mcpgate/internal/contracts"
	"github.com/mcpgate/mcpgate/internal/protocol/legacy"
)

func TestLegacyHTTPInitializeGolden(t *testing.T) {
	handler := legacy.Handler(legacy.Dependencies{ServerID: "public", Sessions: legacy.NewSessions(10, 15*time.Minute, time.Now), Principal: func(*http.Request) (contracts.Principal, bool) {
		return contracts.Principal{PartitionDigest: "p"}, true
	}})
	body := `{"jsonrpc":"2.0","id":"normalized-id","method":"initialize","params":{"protocolVersion":"2025-11-25","clientInfo":{"name":"legacy","version":"1"},"capabilities":{}}}`
	request := httptest.NewRequest(http.MethodPost, "/legacy", strings.NewReader(body))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	expected := "{\"jsonrpc\":\"2.0\",\"id\":\"normalized-id\",\"result\":{\"capabilities\":{\"tools\":{}},\"protocolVersion\":\"2025-11-25\",\"serverInfo\":{\"name\":\"mcpgate\",\"version\":\"v1\"}}}\n"
	if response.Code != 200 || response.Header().Get("Mcp-Session-Id") == "" || response.Body.String() != expected {
		t.Fatalf("status=%d headers=%v\nwant=%s\ngot=%s", response.Code, response.Header(), expected, response.Body.String())
	}
}
