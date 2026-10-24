package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/9OctopusDev/mcpgate/internal/catalog"
	"github.com/9OctopusDev/mcpgate/internal/contracts"
)

type DiscoveryResult struct {
	Version     string
	Catalog     contracts.Catalog
	Fingerprint string
}

func (adapter *Adapter) DetectAndDiscover(parent context.Context) (DiscoveryResult, error) {
	ctx, cancel := context.WithTimeout(parent, adapter.limits.DiscoveryTimeout)
	defer cancel()
	adapter.mu.Lock()
	adapter.version = ModernVersion
	adapter.mu.Unlock()
	discovery, err := adapter.exchange(ctx, "server/discover", map[string]any{"clientInfo": map[string]string{"name": "mcpgate", "version": "v1"}, "capabilities": map[string]any{}}, "", false)
	version := ModernVersion
	if err != nil {
		return DiscoveryResult{}, err
	}
	if discovery.Error != nil {
		if discovery.Error.Code != -32601 {
			return DiscoveryResult{}, fmt.Errorf("modern discovery failed: code %d", discovery.Error.Code)
		}
		version = LegacyVersion
		adapter.mu.Lock()
		adapter.version = LegacyVersion
		adapter.mu.Unlock()
		initialized, err := adapter.exchange(ctx, "initialize", map[string]any{"protocolVersion": LegacyVersion, "clientInfo": map[string]string{"name": "mcpgate", "version": "v1"}, "capabilities": map[string]any{}}, "", false)
		if err != nil || initialized.Error != nil {
			return DiscoveryResult{}, errors.New("legacy initialization failed")
		}
		if _, err := adapter.exchange(ctx, "notifications/initialized", map[string]any{}, "", true); err != nil {
			return DiscoveryResult{}, err
		}
	} else {
		var result struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		if json.Unmarshal(discovery.Result, &result) != nil || result.ProtocolVersion != ModernVersion {
			return DiscoveryResult{}, errors.New("unsupported modern discovery result")
		}
	}
	tools, err := adapter.listTools(ctx)
	if err != nil {
		return DiscoveryResult{}, err
	}
	sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
	digest := sha256.New()
	for i := range tools {
		tools[i].Fingerprint = catalog.Fingerprint(tools[i])
		_, _ = digest.Write([]byte(tools[i].Fingerprint))
	}
	result := DiscoveryResult{Version: version, Catalog: contracts.Catalog{Tools: tools, Protocol: version, Revision: hex.EncodeToString(digest.Sum(nil)), ValidUntil: time.Now().Add(adapter.limits.DiscoveryTimeout)}, Fingerprint: hex.EncodeToString(digest.Sum(nil))}
	adapter.mu.Lock()
	adapter.catalog = result.Catalog
	adapter.mu.Unlock()
	return result, nil
}

func (adapter *Adapter) listTools(ctx context.Context) ([]contracts.ToolContract, error) {
	seenCursors := make(map[string]struct{})
	seenTools := make(map[string]struct{})
	var cursor string
	var tools []contracts.ToolContract
	for page := 0; page < adapter.limits.MaxPages; page++ {
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		response, err := adapter.exchange(ctx, "tools/list", params, "", false)
		if err != nil {
			return nil, err
		}
		if response.Error != nil {
			return nil, fmt.Errorf("tools/list failed: code %d", response.Error.Code)
		}
		var result struct {
			Tools []struct {
				Name, Description string
				InputSchema       json.RawMessage `json:"inputSchema"`
				OutputSchema      json.RawMessage `json:"outputSchema"`
			} `json:"tools"`
			NextCursor string `json:"nextCursor"`
		}
		if err := json.Unmarshal(response.Result, &result); err != nil {
			return nil, errors.New("invalid tools/list result")
		}
		for _, tool := range result.Tools {
			if tool.Name == "" || !validSchema(tool.InputSchema) || (len(tool.OutputSchema) > 0 && !validSchema(tool.OutputSchema)) {
				return nil, errors.New("invalid upstream tool contract")
			}
			if _, duplicate := seenTools[tool.Name]; duplicate {
				return nil, errors.New("duplicate upstream tool name")
			}
			seenTools[tool.Name] = struct{}{}
			tools = append(tools, contracts.ToolContract{Name: tool.Name, Description: tool.Description, InputSchema: append(json.RawMessage(nil), tool.InputSchema...), OutputSchema: append(json.RawMessage(nil), tool.OutputSchema...)})
			if len(tools) > adapter.limits.MaxTools {
				return nil, errors.New("upstream tool count exceeds limit")
			}
		}
		if result.NextCursor == "" {
			return tools, nil
		}
		if _, repeated := seenCursors[result.NextCursor]; repeated {
			return nil, errors.New("upstream repeated discovery cursor")
		}
		seenCursors[result.NextCursor] = struct{}{}
		cursor = result.NextCursor
	}
	return nil, errors.New("upstream discovery page limit exceeded")
}

func validSchema(raw json.RawMessage) bool {
	if len(raw) == 0 || !json.Valid(raw) {
		return false
	}
	var schema map[string]any
	return json.Unmarshal(raw, &schema) == nil
}
