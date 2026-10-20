package integration_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/mcpgate/mcpgate/internal/contracts"
	"github.com/mcpgate/mcpgate/internal/edge"
)

type countingVerifier struct{ calls atomic.Int64 }

func (verifier *countingVerifier) Verify(context.Context, string, string) (contracts.Principal, error) {
	verifier.calls.Add(1)
	return contracts.Principal{PartitionDigest: "p"}, nil
}

func TestGatewayRejectsAmbiguousJSONBeforeAuthenticationOrDispatch(t *testing.T) {
	verifier := &countingVerifier{}
	var dispatched atomic.Int64
	handler, err := edge.New([]edge.Route{{ServerID: "s", Host: "mcp.example", Path: "/mcp", ResourceURI: "https://mcp.example/mcp", Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) { dispatched.Add(1) })}}, verifier, edge.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "https://mcp.example/mcp", strings.NewReader(`{"jsonrpc":"2.0","method":"tools/list","method":"tools/call"}`))
	request.Host = "mcp.example"
	request.Header.Set("Authorization", "Bearer secret")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || verifier.calls.Load() != 0 || dispatched.Load() != 0 {
		t.Fatalf("status=%d auth=%d dispatch=%d", response.Code, verifier.calls.Load(), dispatched.Load())
	}
}
