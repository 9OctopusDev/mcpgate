package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/9OctopusDev/mcpgate/internal/contracts"
)

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data,omitempty"`
	} `json:"error,omitempty"`
}

func (adapter *Adapter) exchange(ctx context.Context, method string, params any, name string, notification bool) (rpcResponse, error) {
	message := map[string]any{"jsonrpc": "2.0", "method": method}
	if !notification {
		message["id"] = newRequestID()
	}
	if params != nil {
		message["params"] = params
	}
	body, err := json.Marshal(message)
	if err != nil {
		return rpcResponse{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, adapter.endpoint, bytes.NewReader(body))
	if err != nil {
		return rpcResponse{}, err
	}
	request.Header = make(http.Header)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("MCP-Protocol-Version", adapter.version)
	request.Header.Set("MCP-Client-Name", "mcpgate")
	request.Header.Set("MCP-Client-Version", "v1")
	if name != "" {
		request.Header.Set("MCP-Name", name)
	}
	if adapter.credential != nil && adapter.credential.ApplyHTTP(request) != nil {
		return rpcResponse{}, errors.New("apply configured upstream credential")
	}
	response, err := adapter.client.Do(request)
	if err != nil {
		return rpcResponse{}, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		return rpcResponse{}, errors.New("configured upstream credential rejected")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return rpcResponse{}, fmt.Errorf("upstream HTTP status %d", response.StatusCode)
	}
	encoded, err := io.ReadAll(io.LimitReader(response.Body, adapter.limits.MaxBytes+1))
	if err != nil {
		return rpcResponse{}, err
	}
	if int64(len(encoded)) > adapter.limits.MaxBytes {
		return rpcResponse{}, errors.New("upstream MCP response exceeds limit")
	}
	if notification && len(bytes.TrimSpace(encoded)) == 0 {
		return rpcResponse{JSONRPC: "2.0"}, nil
	}
	var decoded rpcResponse
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decoded); err != nil || decoded.JSONRPC != "2.0" {
		return rpcResponse{}, errors.New("malformed upstream MCP response")
	}
	return decoded, nil
}

var _ contracts.CredentialBinding
