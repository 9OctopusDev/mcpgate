package router

import (
	"context"
	"errors"
	"testing"

	"github.com/mcpgate/mcpgate/internal/contracts"
)

type denyAuthorizer struct{ calls int }

func (authorizer *denyAuthorizer) Authorize(context.Context, contracts.Principal, contracts.Route) (contracts.Decision, error) {
	authorizer.calls++
	return contracts.Decision{Reason: "tenant denied", Epoch: 2}, nil
}

func (authorizer *denyAuthorizer) RecheckAtDispatch(context.Context, contracts.Principal, contracts.Route, uint64) (contracts.Decision, error) {
	return contracts.Decision{}, nil
}

func TestExactRoutingPreservesLiteralPublicNames(t *testing.T) {
	issueRoute := contracts.Route{Key: contracts.RouteKey{ServerID: "team", PublicName: "issues.search"}, UpstreamID: "issues", NativeName: "search_issues"}
	userRoute := contracts.Route{Key: contracts.RouteKey{ServerID: "team", PublicName: "users.get"}, UpstreamID: "users", NativeName: "getUser"}
	router, err := New([]string{"gateway.example", "localhost:8080"}, []Server{{ID: "team", Path: "/mcp/team"}}, []contracts.Route{issueRoute, userRoute})
	if err != nil {
		t.Fatal(err)
	}

	resolved, ok := router.Resolve("gateway.example", "/mcp/team", "issues.search")
	if !ok || resolved.NativeName != "search_issues" {
		t.Fatalf("literal dotted name did not resolve: %+v %v", resolved, ok)
	}
	for _, request := range []struct{ host, path, name string }{
		{host: "other.example", path: "/mcp/team", name: "issues.search"},
		{host: "gateway.example", path: "/mcp/team/", name: "issues.search"},
		{host: "gateway.example", path: "/mcp/TEAM", name: "issues.search"},
		{host: "gateway.example", path: "/mcp/team", name: "search"},
		{host: "gateway.example", path: "/mcp/team", name: "ISSUES.SEARCH"},
	} {
		if _, ok := router.Resolve(request.host, request.path, request.name); ok {
			t.Fatalf("inexact route unexpectedly resolved: %+v", request)
		}
	}
}

func TestConcealmentReturnsSamePublicErrorForAbsentAndDenied(t *testing.T) {
	route := contracts.Route{Key: contracts.RouteKey{ServerID: "team", PublicName: "private.read"}, PolicyID: "private"}
	router, err := New([]string{"gateway.example"}, []Server{{ID: "team", Path: "/mcp/team"}}, []contracts.Route{route})
	if err != nil {
		t.Fatal(err)
	}
	authorizer := &denyAuthorizer{}
	_, absent := router.ResolveAuthorized(context.Background(), "gateway.example", "/mcp/team", "absent", contracts.Principal{}, authorizer)
	_, denied := router.ResolveAuthorized(context.Background(), "gateway.example", "/mcp/team", "private.read", contracts.Principal{}, authorizer)
	if !errors.Is(absent.Public, ErrUnknownTool) || !errors.Is(denied.Public, ErrUnknownTool) {
		t.Fatalf("public errors differ: absent=%v denied=%v", absent.Public, denied.Public)
	}
	if absent.Cause == denied.Cause || absent.Cause != CauseAbsent || denied.Cause != CauseDenied {
		t.Fatalf("internal audit causes not preserved: absent=%s denied=%s", absent.Cause, denied.Cause)
	}
	if authorizer.calls != 1 {
		t.Fatalf("authorizer calls = %d, want 1", authorizer.calls)
	}
}
