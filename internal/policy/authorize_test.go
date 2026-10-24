package policy

import (
	"context"
	"testing"

	"github.com/9OctopusDev/mcpgate/internal/config"
	"github.com/9OctopusDev/mcpgate/internal/contracts"
)

func policyFixture(t *testing.T) *Engine {
	t.Helper()
	engine, err := Compile(map[string]config.Policy{
		"reader": {
			Tenants:   []string{"tenant-a", "tenant-b"},
			Subjects:  []string{"person-7", "service-2"},
			ScopesAll: []string{"tools:read", "records:list"},
		},
		"empty": {},
	})
	if err != nil {
		t.Fatal(err)
	}
	return engine
}

func TestPolicyANDsPredicatesAndORsValues(t *testing.T) {
	engine := policyFixture(t)
	route := contracts.Route{Key: contracts.RouteKey{ServerID: "team", PublicName: "records.list"}, UpstreamID: "records", PolicyID: "reader"}
	principal := contracts.Principal{Tenant: "tenant-b", Subject: "service-2", Scopes: []string{"records:list", "extra", "tools:read"}, PartitionDigest: "partition-1"}

	decision, err := engine.Authorize(context.Background(), principal, route)
	if err != nil || !decision.Allowed || decision.Epoch == 0 {
		t.Fatalf("expected allow decision, got %+v, %v", decision, err)
	}

	checks := []struct {
		name string
		edit func(*contracts.Principal)
	}{
		{name: "tenant", edit: func(value *contracts.Principal) { value.Tenant = "tenant-c" }},
		{name: "subject", edit: func(value *contracts.Principal) { value.Subject = "person-8" }},
		{name: "scope", edit: func(value *contracts.Principal) { value.Scopes = []string{"tools:read"} }},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			candidate := principal
			check.edit(&candidate)
			decision, err := engine.Authorize(context.Background(), candidate, route)
			if err != nil || decision.Allowed {
				t.Fatalf("expected deny decision, got %+v, %v", decision, err)
			}
		})
	}
}

func TestPolicyDeniesMissingAndEmptyPolicies(t *testing.T) {
	engine := policyFixture(t)
	principal := contracts.Principal{Tenant: "tenant-a", Subject: "person-7", Scopes: []string{"tools:read", "records:list"}}
	for _, policyID := range []string{"missing", "empty", ""} {
		decision, err := engine.Authorize(context.Background(), principal, contracts.Route{PolicyID: policyID})
		if err != nil || decision.Allowed {
			t.Fatalf("policy %q should deny, got %+v, %v", policyID, decision, err)
		}
	}
}

func TestPolicyDispatchRecheckUsesLiveRevocationEpoch(t *testing.T) {
	engine := policyFixture(t)
	route := contracts.Route{Key: contracts.RouteKey{ServerID: "team", PublicName: "records.list"}, UpstreamID: "records", PolicyID: "reader"}
	principal := contracts.Principal{Tenant: "tenant-a", Subject: "person-7", Scopes: []string{"tools:read", "records:list"}, PartitionDigest: "partition-1"}
	initial, _ := engine.Authorize(context.Background(), principal, route)

	engine.RevokePrincipal(principal.PartitionDigest)
	decision, err := engine.RecheckAtDispatch(context.Background(), principal, route, initial.Epoch)
	if err != nil || decision.Allowed || decision.Epoch <= initial.Epoch || decision.Reason != "principal revoked" {
		t.Fatalf("stale principal authorization was not revoked: %+v, %v", decision, err)
	}

	engine.ReplaceOverlay(Overlay{})
	afterReset, _ := engine.Authorize(context.Background(), principal, route)
	engine.RevokeExport(route.Key)
	decision, _ = engine.RecheckAtDispatch(context.Background(), principal, route, afterReset.Epoch)
	if decision.Allowed || decision.Reason != "export revoked" {
		t.Fatalf("stale export authorization was not revoked: %+v", decision)
	}

	engine.ReplaceOverlay(Overlay{})
	afterReset, _ = engine.Authorize(context.Background(), principal, route)
	engine.RevokeUpstream(route.UpstreamID)
	decision, _ = engine.RecheckAtDispatch(context.Background(), principal, route, afterReset.Epoch)
	if decision.Allowed || decision.Reason != "upstream revoked" {
		t.Fatalf("stale upstream authorization was not revoked: %+v", decision)
	}
}

func TestPolicyEpochChangeUnrelatedToRouteReauthorizes(t *testing.T) {
	engine := policyFixture(t)
	route := contracts.Route{Key: contracts.RouteKey{ServerID: "team", PublicName: "records.list"}, UpstreamID: "records", PolicyID: "reader"}
	principal := contracts.Principal{Tenant: "tenant-a", Subject: "person-7", Scopes: []string{"tools:read", "records:list"}, PartitionDigest: "partition-1"}
	initial, _ := engine.Authorize(context.Background(), principal, route)
	engine.RevokeUpstream("another-upstream")
	decision, err := engine.RecheckAtDispatch(context.Background(), principal, route, initial.Epoch)
	if err != nil || !decision.Allowed || decision.Epoch <= initial.Epoch {
		t.Fatalf("unrelated overlay should permit fresh authorization: %+v, %v", decision, err)
	}
}
