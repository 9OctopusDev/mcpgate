package mcp

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/mcpgate/mcpgate/internal/contracts"
)

type StdioInvoker interface {
	Call(context.Context, []byte, []byte) ([]byte, error)
}

type StdioClient struct {
	invoker StdioInvoker
	catalog contracts.Catalog
	release func()
}

func NewStdioClient(invoker StdioInvoker, catalog contracts.Catalog, release func()) (*StdioClient, error) {
	if invoker == nil {
		return nil, errors.New("stdio invoker is required")
	}
	catalog.Tools = append([]contracts.ToolContract(nil), catalog.Tools...)
	return &StdioClient{invoker: invoker, catalog: catalog, release: release}, nil
}

func (client *StdioClient) Discover(context.Context, contracts.CredentialBinding) (contracts.Catalog, error) {
	result := client.catalog
	result.Tools = append([]contracts.ToolContract(nil), result.Tools...)
	return result, nil
}
func (client *StdioClient) CallTool(ctx context.Context, call contracts.Call, _ contracts.EventSink) (contracts.Outcome, error) {
	id := json.RawMessage(`"` + call.ExecutionID + `"`)
	if call.ExecutionID == "" {
		id = json.RawMessage(`"` + newRequestID() + `"`)
	}
	request, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": "tools/call", "params": map[string]any{"name": call.Route.NativeName, "arguments": json.RawMessage(call.Arguments)}})
	if err != nil {
		return contracts.Outcome{}, err
	}
	frame, err := client.invoker.Call(ctx, id, request)
	if err != nil {
		return contracts.Outcome{}, err
	}
	var response rpcResponse
	if json.Unmarshal(frame, &response) != nil || response.JSONRPC != "2.0" {
		return contracts.Outcome{ProtocolError: &contracts.ProtocolError{Code: -32603, Message: "upstream protocol error"}}, nil
	}
	if response.Error != nil {
		return contracts.Outcome{ProtocolError: &contracts.ProtocolError{Code: response.Error.Code, Message: "upstream protocol error"}}, nil
	}
	var result struct {
		Content    []contracts.ContentBlock `json:"content"`
		Structured json.RawMessage          `json:"structuredContent"`
		IsError    bool                     `json:"isError"`
	}
	if json.Unmarshal(response.Result, &result) != nil {
		return contracts.Outcome{ProtocolError: &contracts.ProtocolError{Code: -32603, Message: "upstream protocol error"}}, nil
	}
	return contracts.Outcome{Complete: contracts.NewToolResult(result.Structured, result.Content, result.IsError)}, nil
}
func (client *StdioClient) Close(context.Context) error {
	if client.release != nil {
		client.release()
		client.release = nil
	}
	return nil
}

var _ contracts.ToolAdapter = (*StdioClient)(nil)
