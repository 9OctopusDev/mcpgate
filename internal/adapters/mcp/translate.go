package mcp

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/mcpgate/mcpgate/internal/contracts"
)

func (adapter *Adapter) CallTool(ctx context.Context, call contracts.Call, _ contracts.EventSink) (contracts.Outcome, error) {
	if call.Route.NativeName == "" || !json.Valid(call.Arguments) {
		return contracts.Outcome{ProtocolError: &contracts.ProtocolError{Code: -32602, Message: "invalid tool call"}}, nil
	}
	response, err := adapter.exchange(ctx, "tools/call", map[string]any{"name": call.Route.NativeName, "arguments": json.RawMessage(call.Arguments), "_meta": map[string]any{"io.modelcontextprotocol/protocolVersion": adapter.version, "io.modelcontextprotocol/clientInfo": map[string]string{"name": "mcpgate", "version": "v1"}}}, call.Route.NativeName, false)
	if err != nil {
		return contracts.Outcome{}, err
	}
	if response.Error != nil {
		return contracts.Outcome{ProtocolError: &contracts.ProtocolError{Code: response.Error.Code, Message: "upstream protocol error"}}, nil
	}
	var result struct {
		ResultType string                     `json:"resultType"`
		Content    []contracts.ContentBlock   `json:"content"`
		Structured json.RawMessage            `json:"structuredContent"`
		IsError    bool                       `json:"isError"`
		Metadata   map[string]json.RawMessage `json:"_meta"`
	}
	if err := json.Unmarshal(response.Result, &result); err != nil {
		return contracts.Outcome{ProtocolError: &contracts.ProtocolError{Code: -32603, Message: "upstream protocol error"}}, nil
	}
	if result.ResultType == "input_required" {
		return contracts.Outcome{ProtocolError: &contracts.ProtocolError{Code: -32004, Message: "interactive input is unsupported"}}, nil
	}
	if len(result.Content) == 0 && len(result.Structured) == 0 {
		return contracts.Outcome{}, errors.New("empty upstream tool result")
	}
	toolResult := contracts.NewToolResult(result.Structured, result.Content, result.IsError)
	toolResult.Metadata = result.Metadata
	return contracts.Outcome{Complete: toolResult}, nil
}
