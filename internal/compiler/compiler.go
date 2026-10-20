package compiler

import (
	"context"
	"sort"

	"github.com/mcpgate/mcpgate/internal/config"
	"github.com/mcpgate/mcpgate/internal/contracts"
	gatewayruntime "github.com/mcpgate/mcpgate/internal/runtime"
)

func Compile(ctx context.Context, inputs Inputs) (*gatewayruntime.Generation, []config.Diagnostic) {
	if err := ctx.Err(); err != nil {
		return nil, []config.Diagnostic{{Code: "COMPILE_CANCELLED", File: "<configuration>", Line: 1, Column: 1, ConfigPath: "/", Message: "candidate compilation was cancelled"}}
	}
	if inputs.Document == nil {
		return nil, []config.Diagnostic{{Code: "CFG_DOCUMENT_MISSING", File: "<configuration>", Line: 1, Column: 1, ConfigPath: "/", Message: "configuration document is required"}}
	}
	validator := &validator{inputs: inputs}
	validator.validateReferences()
	validator.validateDuplicates()
	validator.validateEndpoints()
	validator.validateCompatibility()
	if len(validator.diagnostics) > 0 {
		sort.SliceStable(validator.diagnostics, func(i, j int) bool {
			if validator.diagnostics[i].ConfigPath != validator.diagnostics[j].ConfigPath {
				return validator.diagnostics[i].ConfigPath < validator.diagnostics[j].ConfigPath
			}
			if validator.diagnostics[i].Code != validator.diagnostics[j].Code {
				return validator.diagnostics[i].Code < validator.diagnostics[j].Code
			}
			if validator.diagnostics[i].OperationID != validator.diagnostics[j].OperationID {
				return validator.diagnostics[i].OperationID < validator.diagnostics[j].OperationID
			}
			return validator.diagnostics[i].Message < validator.diagnostics[j].Message
		})
		return nil, validator.diagnostics
	}

	routes := make(map[contracts.RouteKey]contracts.Route)
	servers := make(map[string]gatewayruntime.VirtualServer, len(inputs.Document.Servers))
	serverIDs := sortedKeys(inputs.Document.Servers)
	for _, serverID := range serverIDs {
		serverConfig := inputs.Document.Servers[serverID]
		server := gatewayruntime.VirtualServer{
			ID: serverID, Path: serverConfig.Path, ResourceURI: serverConfig.ResourceURI,
			Authentication: serverConfig.Authentication, Protocols: append([]string(nil), serverConfig.Protocols...),
		}
		for _, export := range serverConfig.Exports {
			manifest, available := inputs.Manifests[export.Upstream]
			contract, found := findContract(manifest.Tools, export.Native)
			if !available || !found {
				if inputs.Document.Upstreams[export.Upstream].Required {
					validator.add("CFG_CAPABILITY_MISSING", "/servers/"+serverID+"/exports", export.Native, "required native capability is unavailable")
				}
				continue
			}
			contract.Name = export.Name
			key := contracts.RouteKey{ServerID: serverID, PublicName: export.Name}
			upstream := inputs.Document.Upstreams[export.Upstream]
			routes[key] = contracts.Route{
				Key: key, UpstreamID: export.Upstream, NativeName: export.Native, PolicyID: export.Policy,
				Contract: contract, Behavior: contracts.Behavior(export.Behavior), Timeout: upstream.Execution.Timeout.Duration,
				MaxAttempts: upstream.Execution.MaxAttempts,
			}
			server.RouteKeys = append(server.RouteKeys, key)
		}
		sort.Slice(server.RouteKeys, func(i, j int) bool { return server.RouteKeys[i].PublicName < server.RouteKeys[j].PublicName })
		servers[serverID] = server
	}
	if len(validator.diagnostics) > 0 {
		return nil, validator.diagnostics
	}
	revision := computeRevision(inputs, routes)
	return gatewayruntime.NewGeneration(gatewayruntime.GenerationSpec{
		Revision: revision, Routes: routes, Servers: servers, DrainTimeout: inputs.Document.Gateway.Lifecycle.DrainTimeout.Duration,
		Resources: append([]*gatewayruntime.SharedResource(nil), inputs.Resources...),
	}), nil
}

func findContract(tools []contracts.ToolContract, name string) (contracts.ToolContract, bool) {
	for _, tool := range tools {
		if tool.Name == name {
			return tool, true
		}
	}
	return contracts.ToolContract{}, false
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
