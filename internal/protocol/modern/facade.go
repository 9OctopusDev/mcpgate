package modern

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/9OctopusDev/mcpgate/internal/contracts"
	"github.com/9OctopusDev/mcpgate/internal/executor"
)

type wireRequest struct {
	JSONRPC  string          `json:"jsonrpc"`
	ID       json.RawMessage `json:"id"`
	Method   string          `json:"method"`
	Params   json.RawMessage `json:"params"`
	Metadata Metadata        `json:"_meta"`
}

func Handler(dependencies Dependencies) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Cache-Control", "no-store")
		var rpc wireRequest
		decoder := json.NewDecoder(request.Body)
		decoder.UseNumber()
		if err := decoder.Decode(&rpc); err != nil || rpc.JSONRPC != "2.0" || len(rpc.ID) == 0 || rpc.Method == "" {
			writeJSON(writer, http.StatusBadRequest, errorResponse(rpc.ID, -32600, "invalid request"))
			return
		}
		var trailing any
		if err := decoder.Decode(&trailing); err != io.EOF {
			writeJSON(writer, http.StatusBadRequest, errorResponse(rpc.ID, -32600, "invalid request"))
			return
		}
		if err := validateMetadata(request, rpc.Metadata); err != nil {
			writeJSON(writer, http.StatusBadRequest, errorResponse(rpc.ID, -32600, "invalid protocol metadata"))
			return
		}
		principal, authenticated := principalFor(dependencies, request)
		if !authenticated && rpc.Method != "server/discover" {
			writeJSON(writer, http.StatusUnauthorized, errorResponse(rpc.ID, -32001, "unauthorized"))
			return
		}
		switch rpc.Method {
		case "server/discover":
			writeJSON(writer, http.StatusOK, response{JSONRPC: "2.0", ID: rpc.ID, Result: map[string]any{"protocolVersion": ProtocolVersion, "serverInfo": map[string]string{"name": "mcpgate", "version": "v1"}, "capabilities": map[string]any{"tools": map[string]any{}}}})
		case "tools/list":
			if dependencies.List == nil {
				writeJSON(writer, http.StatusServiceUnavailable, errorResponse(rpc.ID, -32603, "catalog unavailable"))
				return
			}
			var params struct {
				Cursor string `json:"cursor"`
			}
			if len(rpc.Params) > 0 {
				_ = json.Unmarshal(rpc.Params, &params)
			}
			page, err := dependencies.List(request.Context(), principal, params.Cursor)
			if err != nil {
				writeJSON(writer, http.StatusServiceUnavailable, errorResponse(rpc.ID, -32603, "catalog unavailable"))
				return
			}
			writeJSON(writer, http.StatusOK, response{JSONRPC: "2.0", ID: rpc.ID, Result: page})
		case "tools/call":
			if dependencies.Resolve == nil || dependencies.Execute == nil {
				writeJSON(writer, http.StatusServiceUnavailable, errorResponse(rpc.ID, -32603, "executor unavailable"))
				return
			}
			var params struct {
				Name string `json:"name"`
			}
			_ = json.Unmarshal(rpc.Params, &params)
			if params.Name == "" || request.Header.Get("MCP-Name") != params.Name {
				writeJSON(writer, http.StatusBadRequest, errorResponse(rpc.ID, -32602, "tool name metadata mismatch"))
				return
			}
			execute := func(sink contracts.EventSink) (contracts.Outcome, error) {
				var parameters struct {
					Name      string          `json:"name"`
					Arguments json.RawMessage `json:"arguments"`
				}
				if err := json.Unmarshal(rpc.Params, &parameters); err != nil {
					return contracts.Outcome{ProtocolError: &contracts.ProtocolError{Code: -32602, Message: "invalid parameters"}}, nil
				}
				route, found := dependencies.Resolve(parameters.Name)
				if !found {
					return contracts.Outcome{ProtocolError: &contracts.ProtocolError{Code: -32601, Message: "tool not found"}}, nil
				}
				return dependencies.Execute.Execute(request.Context(), executor.Request{Route: route, Principal: principal, Arguments: parameters.Arguments, RequestID: rpc.ID}, sink)
			}
			if strings.Contains(request.Header.Get("Accept"), "text/event-stream") {
				writeSSE(writer, request, execute, rpc.ID)
				return
			}
			outcome, err := execute(nil)
			if err != nil {
				writeJSON(writer, http.StatusOK, errorResponse(rpc.ID, -32603, "tool execution failed"))
				return
			}
			writeJSON(writer, http.StatusOK, outcomeResponse(rpc.ID, outcome))
		case "notifications/cancelled":
			if dependencies.Cancel == nil {
				writeJSON(writer, http.StatusOK, response{JSONRPC: "2.0", ID: rpc.ID, Result: map[string]any{}})
				return
			}
			var params struct {
				RequestID json.RawMessage `json:"requestId"`
				Session   string          `json:"session"`
			}
			_ = json.Unmarshal(rpc.Params, &params)
			dependencies.Cancel(executor.CorrelationKey{Principal: principal.PartitionDigest, Session: params.Session, Request: string(params.RequestID)})
			writeJSON(writer, http.StatusOK, response{JSONRPC: "2.0", ID: rpc.ID, Result: map[string]any{}})
		default:
			writeJSON(writer, http.StatusNotFound, errorResponse(rpc.ID, -32601, "method not found"))
		}
	})
}

func outcomeResponse(id json.RawMessage, outcome contracts.Outcome) response {
	if outcome.ProtocolError != nil {
		return errorResponse(id, outcome.ProtocolError.Code, outcome.ProtocolError.Message)
	}
	if outcome.InputRequired != nil {
		return errorResponse(id, -32004, "interactive input is unsupported")
	}
	if outcome.Complete == nil {
		return errorResponse(id, -32603, "invalid tool outcome")
	}
	return response{JSONRPC: "2.0", ID: id, Result: map[string]any{"resultType": "complete", "content": outcome.Complete.Content, "structuredContent": json.RawMessage(outcome.Complete.Structured), "isError": outcome.Complete.IsError, "_meta": outcome.Complete.Metadata}}
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}
