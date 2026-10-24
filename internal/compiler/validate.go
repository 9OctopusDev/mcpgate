package compiler

import (
	"net/netip"
	"net/url"
	"strings"

	"github.com/9OctopusDev/mcpgate/internal/config"
	"github.com/9OctopusDev/mcpgate/internal/contracts"
)

const modernProtocol = "2026-07-28"

type validator struct {
	inputs      Inputs
	diagnostics []config.Diagnostic
}

func (validator *validator) add(code, path, operation, message string) {
	file := "<configuration>"
	if validator.inputs.Document != nil && validator.inputs.Document.Source.Requested != "" {
		file = validator.inputs.Document.Source.Requested
	}
	validator.diagnostics = append(validator.diagnostics, config.Diagnostic{
		Code: code, File: file, Line: 1, Column: 1, ConfigPath: path, OperationID: operation, Message: message,
	})
}

func (validator *validator) validateReferences() {
	document := validator.inputs.Document
	for serverID, server := range document.Servers {
		base := "/servers/" + serverID
		if _, ok := document.Authentication[server.Authentication]; !ok {
			validator.add("CFG-02", base+"/authentication", "", "referenced authenticator does not exist")
		}
		if _, ok := document.Secrets[server.Catalog.CursorSecret]; !ok {
			validator.add("CFG-02", base+"/catalog/cursor_secret", "", "referenced cursor secret does not exist")
		} else if _, ok := validator.inputs.CredentialIdentities[server.Catalog.CursorSecret]; !ok {
			validator.add("CFG-02", base+"/catalog/cursor_secret", "", "referenced cursor credential is unavailable")
		}
		for index, export := range server.Exports {
			path := base + "/exports/" + intString(index)
			if _, ok := document.Policies[export.Policy]; !ok {
				validator.add("CFG-02", path+"/policy", export.Native, "referenced policy does not exist")
			}
			if _, ok := document.Upstreams[export.Upstream]; !ok {
				validator.add("CFG-02", path+"/upstream", export.Native, "referenced upstream does not exist")
			}
		}
	}
	for upstreamID, upstream := range document.Upstreams {
		if upstream.Auth.Secret != "" {
			if _, ok := document.Secrets[upstream.Auth.Secret]; !ok {
				validator.add("CFG-02", "/upstreams/"+upstreamID+"/auth/secret", "", "referenced upstream secret does not exist")
			} else if _, ok := validator.inputs.CredentialIdentities[upstream.Auth.Secret]; !ok {
				validator.add("CFG-02", "/upstreams/"+upstreamID+"/auth/secret", "", "referenced upstream credential is unavailable")
			}
		}
		if upstream.Kind == "rest" && upstream.OpenAPI.ExternalRefs != "deny" {
			validator.add("CFG-04", "/upstreams/"+upstreamID+"/openapi/external_refs", "", "external OpenAPI references are disabled")
		}
	}
}

func (validator *validator) validateDuplicates() {
	document := validator.inputs.Document
	paths := make(map[string]string)
	for _, serverID := range sortedKeys(document.Servers) {
		server := document.Servers[serverID]
		if previous, exists := paths[server.Path]; exists {
			validator.add("CFG-03", "/servers/"+serverID+"/path", "", "server path duplicates "+previous)
		} else {
			paths[server.Path] = serverID
		}
		names := make(map[string]struct{})
		for index, export := range server.Exports {
			if _, exists := names[export.Name]; exists {
				validator.add("CFG-03", "/servers/"+serverID+"/exports/"+intString(index)+"/name", export.Native, "public tool name is duplicated")
			}
			names[export.Name] = struct{}{}
		}
	}
	for _, upstreamID := range sortedKeys(document.Upstreams) {
		upstream := document.Upstreams[upstreamID]
		endpointIDs := make(map[string]struct{})
		for index, endpoint := range upstream.Endpoints {
			if _, exists := endpointIDs[endpoint.ID]; exists {
				validator.add("CFG-03", "/upstreams/"+upstreamID+"/endpoints/"+intString(index)+"/id", "", "endpoint id is duplicated")
			}
			endpointIDs[endpoint.ID] = struct{}{}
		}
		toolNames := make(map[string]struct{})
		for _, tool := range validator.inputs.Manifests[upstreamID].Tools {
			if _, exists := toolNames[tool.Name]; exists {
				validator.add("CFG-03", "/upstreams/"+upstreamID, tool.Name, "native operation id is duplicated")
			}
			toolNames[tool.Name] = struct{}{}
		}
	}
}

func (validator *validator) validateEndpoints() {
	for upstreamID, upstream := range validator.inputs.Document.Upstreams {
		if upstream.Kind != "rest" && upstream.Kind != "mcp_http" {
			continue
		}
		allowedHosts := make(map[string]struct{}, len(upstream.Network.AllowedHosts))
		for _, host := range upstream.Network.AllowedHosts {
			allowedHosts[strings.ToLower(strings.TrimSuffix(host, "."))] = struct{}{}
		}
		if !validAddressPolicy(upstream.Network.AllowedCIDRs) {
			validator.add("CFG_CIDR_INVALID", "/upstreams/"+upstreamID+"/network/allowed_cidrs", "", "destination address policy is invalid")
		}
		for index, endpoint := range upstream.Endpoints {
			parsed, err := url.Parse(endpoint.URL)
			host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
			_, hostAllowed := allowedHosts[host]
			if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || !hostAllowed {
				validator.add("CFG_ENDPOINT_INVALID", "/upstreams/"+upstreamID+"/endpoints/"+intString(index)+"/url", "", "remote endpoint violates the configured trust boundary")
			}
		}
	}
}

func validAddressPolicy(raw any) bool {
	switch value := raw.(type) {
	case string:
		return value == "public_only"
	case []string:
		if len(value) == 0 {
			return false
		}
		for _, item := range value {
			if _, err := netip.ParsePrefix(item); err != nil {
				return false
			}
		}
		return true
	case []any:
		if len(value) == 0 {
			return false
		}
		for _, rawItem := range value {
			item, ok := rawItem.(string)
			if !ok {
				return false
			}
			if _, err := netip.ParsePrefix(item); err != nil {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func (validator *validator) validateCompatibility() {
	for serverID, server := range validator.inputs.Document.Servers {
		if server.Features.ListChanged || server.Features.InteractiveBridge || server.Features.Resources || server.Features.Prompts {
			validator.add("CFG-08", "/servers/"+serverID+"/features", "", "advanced MCP features are not supported in v1")
		}
		modern := contains(server.Protocols, modernProtocol)
		for index, export := range server.Exports {
			upstream, exists := validator.inputs.Document.Upstreams[export.Upstream]
			if !exists {
				continue
			}
			path := "/servers/" + serverID + "/exports/" + intString(index)
			if modern && upstream.StateModel == "implicit_session" {
				validator.add("CFG-07", path, export.Native, "implicit upstream state cannot be exposed through the modern protocol")
			}
			if upstream.Execution.MaxAttempts > 1 && export.Behavior != string(contracts.BehaviorReadOnly) {
				validator.add("CFG_RETRY_UNSAFE", path+"/behavior", export.Native, "multiple attempts require an explicitly read-only export")
			}
			if upstream.Kind == "rest" && !validator.inputs.SecurityResolved[export.Upstream] {
				validator.add("CFG_SECURITY_UNRESOLVED", path, export.Native, "OpenAPI security requirements are not satisfied")
			}
		}
	}
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func intString(value int) string {
	if value == 0 {
		return "0"
	}
	const digits = "0123456789"
	buffer := [20]byte{}
	position := len(buffer)
	for value > 0 {
		position--
		buffer[position] = digits[value%10]
		value /= 10
	}
	return string(buffer[position:])
}
