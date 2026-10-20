package edge

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/mcpgate/mcpgate/internal/contracts"
)

type verifierFunc func(context.Context, string, string) (contracts.Principal, error)

func (fn verifierFunc) Verify(ctx context.Context, token, resource string) (contracts.Principal, error) {
	return fn(ctx, token, resource)
}

func TestEdgeRejectsAuthorityOriginBoundsAndDuplicateMembersBeforeDispatch(t *testing.T) {
	var called atomic.Int64
	verifier := verifierFunc(func(context.Context, string, string) (contracts.Principal, error) {
		return contracts.Principal{PartitionDigest: "p"}, nil
	})
	handler, err := New([]Route{{ServerID: "s", Host: "mcp.example", Path: "/mcp", ResourceURI: "https://mcp.example/mcp", Origins: []string{"https://app.example"}, Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called.Add(1) })}}, verifier, Limits{HeaderBytes: 1024, BodyBytes: 1024, JSONDepth: 16, ParseTimeout: 1000000000})
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct{ name, host, path, origin, body string }{
		{"host", "evil.example", "/mcp", "https://app.example", `{}`},
		{"path", "mcp.example", "/mcp/", "https://app.example", `{}`},
		{"origin", "mcp.example", "/mcp", "https://evil.example", `{}`},
		{"duplicate", "mcp.example", "/mcp", "https://app.example", `{"method":"tools/list","method":"tools/call"}`},
		{"oversize", "mcp.example", "/mcp", "https://app.example", `{"x":"` + strings.Repeat("a", 1024) + `"}`},
	}
	for _, test := range cases {
		req := httptest.NewRequest(http.MethodPost, "https://"+test.host+test.path, strings.NewReader(test.body))
		req.Host = test.host
		req.Header.Set("Origin", test.origin)
		req.Header.Set("Authorization", "Bearer token")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		if response.Code < 400 {
			t.Fatalf("%s accepted with %d", test.name, response.Code)
		}
	}
	if called.Load() != 0 {
		t.Fatalf("downstream called %d times", called.Load())
	}
}

func TestEdgeAuthenticatesAndForwardsBoundedBody(t *testing.T) {
	verifier := verifierFunc(func(_ context.Context, token, resource string) (contracts.Principal, error) {
		if token != "token" || resource != "https://mcp.example/mcp" {
			t.Fatalf("token=%q resource=%q", token, resource)
		}
		return contracts.Principal{PartitionDigest: "partition"}, nil
	})
	handler, err := New([]Route{{ServerID: "s", Host: "mcp.example", Path: "/mcp", ResourceURI: "https://mcp.example/mcp", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		principal, ok := PrincipalFromContext(r.Context())
		if !ok || principal.PartitionDigest != "partition" {
			t.Fatal("principal missing")
		}
		w.WriteHeader(http.StatusNoContent)
	})}}, verifier, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "https://mcp.example/mcp", strings.NewReader(`{"jsonrpc":"2.0"}`))
	req.Host = "mcp.example"
	req.Header.Set("Authorization", "Bearer token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	if response.Code != http.StatusNoContent {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}
