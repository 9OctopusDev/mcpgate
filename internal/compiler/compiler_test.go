package compiler

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/9OctopusDev/mcpgate/internal/config"
	"github.com/9OctopusDev/mcpgate/internal/contracts"
)

func validDocument() *config.Document {
	return &config.Document{
		APIVersion: "mcpgate/v1",
		Gateway: config.Gateway{
			Data:      config.DataListener{AllowedHosts: []string{"gateway.example"}},
			Lifecycle: config.Lifecycle{DrainTimeout: config.Duration{Duration: time.Second}},
		},
		Secrets: map[string]config.SecretRef{
			"api-token": {Source: "env", Name: "API_TOKEN"},
			"cursor":    {Source: "env", Name: "CURSOR_KEY"},
		},
		Authentication: map[string]config.Authentication{
			"corp": {Kind: "oauth_resource_server", Issuer: "https://issuer.example", JWKSURI: "https://issuer.example/jwks", Algorithms: []string{"RS256"}, TenantClaim: "tenant", ScopeClaim: "scope"},
		},
		Policies: map[string]config.Policy{"reader": {Tenants: []string{"tenant-a"}, ScopesAll: []string{"tools:read"}}},
		Upstreams: map[string]config.Upstream{
			"api": {
				Kind: "rest", Required: true,
				Endpoints:  []config.Endpoint{{ID: "a", URL: "https://api.example/v1", Weight: 1}},
				Network:    config.Network{AllowedHosts: []string{"api.example"}, AllowedCIDRs: "public_only"},
				Auth:       config.UpstreamAuth{Kind: "bearer", Secret: "api-token"},
				OpenAPI:    config.OpenAPI{File: "./api.yaml", ExternalRefs: "deny"},
				StateModel: "request_scoped",
				Execution:  config.UpstreamExecution{Timeout: config.Duration{Duration: time.Second}, MaxInflight: 4, MaxAttempts: 1},
			},
		},
		Servers: map[string]config.Server{
			"team": {
				Path: "/mcp/team", ResourceURI: "https://gateway.example/mcp/team", Authentication: "corp",
				Protocols: []string{"2026-07-28"}, Features: config.ServerFeatures{Tools: true},
				Catalog: config.Catalog{PageSize: 100, CursorSecret: "cursor"},
				Exports: []config.Export{{Name: "records.list", Upstream: "api", Native: "listRecords", Policy: "reader", ServiceIdentityAcknowledged: true, Behavior: "read_only"}},
			},
		},
		Source: config.SourceRef{Requested: "gateway.yaml", Resolved: "/safe/gateway.yaml"},
	}
}

func validInputs(document *config.Document) Inputs {
	return Inputs{
		Document: document,
		CredentialIdentities: map[string]contracts.CredentialIdentity{
			"api-token": {Reference: "env:API_TOKEN", Epoch: 3},
			"cursor":    {Reference: "env:CURSOR_KEY", Epoch: 2},
		},
		Manifests: map[string]contracts.Catalog{
			"api": {Revision: "openapi-rev", Tools: []contracts.ToolContract{{Name: "listRecords", Description: "List records", InputSchema: []byte(`{"type":"object"}`), OutputSchema: []byte(`{"type":"object"}`), Fingerprint: "contract-1"}}},
		},
		SecurityResolved: map[string]bool{"api": true},
	}
}

func diagnosticCodes(diagnostics []config.Diagnostic) string {
	values := make([]string, len(diagnostics))
	for index, diagnostic := range diagnostics {
		values[index] = diagnostic.Code
	}
	return strings.Join(values, ",")
}

func TestSemanticMissingReferencesAreRejected(t *testing.T) {
	tests := []struct {
		name string
		edit func(*config.Document)
	}{
		{name: "policy", edit: func(document *config.Document) { document.Servers["team"].Exports[0].Policy = "missing" }},
		{name: "upstream", edit: func(document *config.Document) { document.Servers["team"].Exports[0].Upstream = "missing" }},
		{name: "authenticator", edit: func(document *config.Document) {
			server := document.Servers["team"]
			server.Authentication = "missing"
			document.Servers["team"] = server
		}},
		{name: "cursor secret", edit: func(document *config.Document) {
			server := document.Servers["team"]
			server.Catalog.CursorSecret = "missing"
			document.Servers["team"] = server
		}},
		{name: "upstream secret", edit: func(document *config.Document) {
			upstream := document.Upstreams["api"]
			upstream.Auth.Secret = "missing"
			document.Upstreams["api"] = upstream
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			document := validDocument()
			test.edit(document)
			generation, diagnostics := Compile(context.Background(), validInputs(document))
			if generation != nil || !strings.Contains(diagnosticCodes(diagnostics), "CFG-02") {
				t.Fatalf("expected CFG-02 rejection, generation=%v diagnostics=%+v", generation, diagnostics)
			}
		})
	}
}

func TestSemanticUnresolvedCredentialIsRejected(t *testing.T) {
	document := validDocument()
	inputs := validInputs(document)
	delete(inputs.CredentialIdentities, "api-token")
	generation, diagnostics := Compile(context.Background(), inputs)
	if generation != nil || !strings.Contains(diagnosticCodes(diagnostics), "CFG-02") {
		t.Fatalf("expected unresolved credential rejection, generation=%v diagnostics=%+v", generation, diagnostics)
	}
}

func TestSemanticDuplicateIdentifiersAreRejected(t *testing.T) {
	tests := []struct {
		name string
		edit func(*config.Document, *Inputs)
	}{
		{name: "server path", edit: func(document *config.Document, _ *Inputs) {
			duplicate := document.Servers["team"]
			document.Servers["other"] = duplicate
		}},
		{name: "public name", edit: func(document *config.Document, _ *Inputs) {
			server := document.Servers["team"]
			server.Exports = append(server.Exports, server.Exports[0])
			document.Servers["team"] = server
		}},
		{name: "endpoint id", edit: func(document *config.Document, _ *Inputs) {
			upstream := document.Upstreams["api"]
			upstream.Endpoints = append(upstream.Endpoints, upstream.Endpoints[0])
			document.Upstreams["api"] = upstream
		}},
		{name: "operation id", edit: func(_ *config.Document, inputs *Inputs) {
			catalog := inputs.Manifests["api"]
			catalog.Tools = append(catalog.Tools, catalog.Tools[0])
			inputs.Manifests["api"] = catalog
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			document := validDocument()
			inputs := validInputs(document)
			test.edit(document, &inputs)
			generation, diagnostics := Compile(context.Background(), inputs)
			if generation != nil || !strings.Contains(diagnosticCodes(diagnostics), "CFG-03") {
				t.Fatalf("expected CFG-03 rejection, generation=%v diagnostics=%+v", generation, diagnostics)
			}
		})
	}
}

func TestSemanticEndpointTrustBoundaries(t *testing.T) {
	tests := []struct {
		name  string
		url   string
		hosts []string
	}{
		{name: "credentials", url: "https://user:password@api.example/v1", hosts: []string{"api.example"}},
		{name: "query", url: "https://api.example/v1?target=other", hosts: []string{"api.example"}},
		{name: "fragment", url: "https://api.example/v1#part", hosts: []string{"api.example"}},
		{name: "cleartext", url: "http://api.example/v1", hosts: []string{"api.example"}},
		{name: "host outside allowlist", url: "https://api.example/v1", hosts: []string{"other.example"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			document := validDocument()
			upstream := document.Upstreams["api"]
			upstream.Endpoints[0].URL = test.url
			upstream.Network.AllowedHosts = test.hosts
			document.Upstreams["api"] = upstream
			generation, diagnostics := Compile(context.Background(), validInputs(document))
			if generation != nil || !strings.Contains(diagnosticCodes(diagnostics), "CFG_ENDPOINT_INVALID") {
				t.Fatalf("expected endpoint rejection, generation=%v diagnostics=%+v", generation, diagnostics)
			}
		})
	}
}

func TestSemanticProtocolFeaturesRetriesAndSecurity(t *testing.T) {
	tests := []struct {
		name string
		code string
		edit func(*config.Document, *Inputs)
	}{
		{name: "implicit state modern", code: "CFG-07", edit: func(document *config.Document, _ *Inputs) {
			upstream := document.Upstreams["api"]
			upstream.StateModel = "implicit_session"
			document.Upstreams["api"] = upstream
		}},
		{name: "resources feature", code: "CFG-08", edit: func(document *config.Document, _ *Inputs) {
			server := document.Servers["team"]
			server.Features.Resources = true
			document.Servers["team"] = server
		}},
		{name: "prompts feature", code: "CFG-08", edit: func(document *config.Document, _ *Inputs) {
			server := document.Servers["team"]
			server.Features.Prompts = true
			document.Servers["team"] = server
		}},
		{name: "list changed feature", code: "CFG-08", edit: func(document *config.Document, _ *Inputs) {
			server := document.Servers["team"]
			server.Features.ListChanged = true
			document.Servers["team"] = server
		}},
		{name: "mutating retry", code: "CFG_RETRY_UNSAFE", edit: func(document *config.Document, _ *Inputs) {
			upstream := document.Upstreams["api"]
			upstream.Execution.MaxAttempts = 2
			document.Upstreams["api"] = upstream
			server := document.Servers["team"]
			server.Exports[0].Behavior = "mutating"
			document.Servers["team"] = server
		}},
		{name: "unresolved security", code: "CFG_SECURITY_UNRESOLVED", edit: func(_ *config.Document, inputs *Inputs) { inputs.SecurityResolved["api"] = false }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			document := validDocument()
			inputs := validInputs(document)
			test.edit(document, &inputs)
			generation, diagnostics := Compile(context.Background(), inputs)
			if generation != nil || !strings.Contains(diagnosticCodes(diagnostics), test.code) {
				t.Fatalf("expected %s rejection, generation=%v diagnostics=%+v", test.code, generation, diagnostics)
			}
		})
	}
}

func TestRevisionIsDeterministicAndNeverContainsSecrets(t *testing.T) {
	const secret = "top-secret-material"
	documentA := validDocument()
	inputsA := validInputs(documentA)
	inputsA.CredentialIdentities["api-token"] = contracts.CredentialIdentity{Reference: "env:API_TOKEN", Epoch: 9}
	generationA, diagnosticsA := Compile(context.Background(), inputsA)
	if generationA == nil || len(diagnosticsA) != 0 {
		t.Fatalf("valid candidate rejected: %+v", diagnosticsA)
	}

	documentB := validDocument()
	inputsB := validInputs(documentB)
	inputsB.CredentialIdentities = map[string]contracts.CredentialIdentity{
		"cursor":    {Reference: "env:CURSOR_KEY", Epoch: 2},
		"api-token": {Reference: "env:API_TOKEN", Epoch: 9},
	}
	generationB, diagnosticsB := Compile(context.Background(), inputsB)
	if generationB == nil || len(diagnosticsB) != 0 {
		t.Fatalf("valid reordered candidate rejected: %+v", diagnosticsB)
	}
	if generationA.Revision() != generationB.Revision() {
		t.Fatalf("revision depends on map order: %s != %s", generationA.Revision(), generationB.Revision())
	}
	for _, text := range []string{generationA.Revision(), fmt.Sprint(diagnosticsA), fmt.Sprint(diagnosticsB)} {
		if strings.Contains(text, secret) {
			t.Fatalf("secret leaked from compiler output: %s", text)
		}
	}
	documentB.Policies["reader"] = config.Policy{Tenants: []string{"tenant-b"}, ScopesAll: []string{"tools:read"}}
	generationPolicy, diagnosticsPolicy := Compile(context.Background(), inputsB)
	if generationPolicy == nil || len(diagnosticsPolicy) != 0 || generationPolicy.Revision() == generationA.Revision() {
		t.Fatalf("policy change was not represented in revision: generation=%v diagnostics=%+v", generationPolicy, diagnosticsPolicy)
	}
	documentB.Policies["reader"] = config.Policy{Tenants: []string{"tenant-a"}, ScopesAll: []string{"tools:read"}}

	inputsB.CredentialIdentities["api-token"] = contracts.CredentialIdentity{Reference: "env:API_TOKEN", Epoch: 10}
	generationC, diagnosticsC := Compile(context.Background(), inputsB)
	if generationC == nil || len(diagnosticsC) != 0 || generationC.Revision() == generationA.Revision() {
		t.Fatalf("credential epoch was not represented in revision: generation=%v diagnostics=%+v", generationC, diagnosticsC)
	}
}
