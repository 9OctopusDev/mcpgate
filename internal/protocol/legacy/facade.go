package legacy

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/9OctopusDev/mcpgate/internal/contracts"
	"github.com/9OctopusDev/mcpgate/internal/edge"
	"github.com/9OctopusDev/mcpgate/internal/executor"
)

type Executor interface {
	Execute(context.Context, executor.Request, contracts.EventSink) (contracts.Outcome, error)
}
type Dependencies struct {
	ServerID    string
	Sessions    *Sessions
	Principal   func(*http.Request) (contracts.Principal, bool)
	Resolve     func(string) (contracts.Route, bool)
	List        func(context.Context, contracts.Principal) ([]contracts.ToolContract, error)
	Execute     Executor
	Cancel      func(executor.CorrelationKey) bool
	CallTimeout time.Duration
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}
type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

func Handler(dependencies Dependencies) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Cache-Control", "no-store")
		if request.Method != http.MethodPost {
			writer.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		principal, ok := principalFor(dependencies, request)
		if !ok {
			write(writer, http.StatusUnauthorized, failure(nil, -32001, "unauthorized"))
			return
		}
		var rpc rpcRequest
		decoder := json.NewDecoder(request.Body)
		decoder.UseNumber()
		if decoder.Decode(&rpc) != nil || rpc.JSONRPC != "2.0" || rpc.Method == "" {
			write(writer, http.StatusBadRequest, failure(rpc.ID, -32600, "invalid request"))
			return
		}
		var trailing any
		if decoder.Decode(&trailing) != io.EOF {
			write(writer, http.StatusBadRequest, failure(rpc.ID, -32600, "invalid request"))
			return
		}
		if rpc.Method == "initialize" {
			initialize(writer, dependencies, principal, rpc)
			return
		}
		sessionID := request.Header.Get("Mcp-Session-Id")
		if dependencies.Sessions == nil {
			write(writer, http.StatusServiceUnavailable, failure(rpc.ID, -32603, "session service unavailable"))
			return
		}
		session, valid := dependencies.Sessions.Get(sessionID, dependencies.ServerID, principal)
		if !valid {
			write(writer, http.StatusUnauthorized, failure(rpc.ID, -32001, "invalid session"))
			return
		}
		if rpc.Method == "notifications/initialized" {
			dependencies.Sessions.MarkInitialized(session.ID)
			writer.WriteHeader(http.StatusNoContent)
			return
		}
		if !session.Initialized {
			write(writer, http.StatusConflict, failure(rpc.ID, -32002, "session not initialized"))
			return
		}
		switch rpc.Method {
		case "ping":
			write(writer, http.StatusOK, success(rpc.ID, map[string]any{}))
		case "tools/list":
			if dependencies.List == nil {
				write(writer, http.StatusServiceUnavailable, failure(rpc.ID, -32603, "catalog unavailable"))
				return
			}
			tools, err := dependencies.List(request.Context(), principal)
			if err != nil {
				write(writer, http.StatusServiceUnavailable, failure(rpc.ID, -32603, "catalog unavailable"))
				return
			}
			write(writer, http.StatusOK, success(rpc.ID, map[string]any{"tools": CompatibleTools(tools)}))
		case "tools/call":
			call(writer, request, dependencies, principal, session, rpc)
		case "notifications/cancelled":
			var params struct {
				RequestID json.RawMessage `json:"requestId"`
			}
			_ = json.Unmarshal(rpc.Params, &params)
			if dependencies.Cancel != nil {
				dependencies.Cancel(cancellationKey(principal.PartitionDigest, session.ID, string(params.RequestID)))
			}
			writer.WriteHeader(http.StatusNoContent)
		default:
			write(writer, http.StatusNotFound, failure(rpc.ID, -32601, "method not found"))
		}
	})
}

func initialize(writer http.ResponseWriter, dependencies Dependencies, principal contracts.Principal, rpc rpcRequest) {
	if dependencies.Sessions == nil {
		write(writer, http.StatusServiceUnavailable, failure(rpc.ID, -32603, "session service unavailable"))
		return
	}
	var params initializeParams
	if json.Unmarshal(rpc.Params, &params) != nil || params.ProtocolVersion != ProtocolVersion || params.ClientInfo.Name == "" || params.ClientInfo.Version == "" {
		write(writer, http.StatusBadRequest, failure(rpc.ID, -32602, "invalid initialize parameters"))
		return
	}
	capabilities := make(map[string]bool)
	for name := range params.Capabilities {
		capabilities[name] = true
	}
	session, err := dependencies.Sessions.Create(dependencies.ServerID, principal, capabilities)
	if err != nil {
		write(writer, http.StatusServiceUnavailable, failure(rpc.ID, -32003, "session capacity reached"))
		return
	}
	writer.Header().Set("Mcp-Session-Id", session.ID)
	write(writer, http.StatusOK, success(rpc.ID, map[string]any{"protocolVersion": ProtocolVersion, "serverInfo": map[string]string{"name": "mcpgate", "version": "v1"}, "capabilities": map[string]any{"tools": map[string]any{}}}))
}

func call(writer http.ResponseWriter, request *http.Request, dependencies Dependencies, principal contracts.Principal, session Session, rpc rpcRequest) {
	if dependencies.Resolve == nil || dependencies.Execute == nil {
		write(writer, http.StatusServiceUnavailable, failure(rpc.ID, -32603, "executor unavailable"))
		return
	}
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if json.Unmarshal(rpc.Params, &params) != nil || params.Name == "" {
		write(writer, http.StatusBadRequest, failure(rpc.ID, -32602, "invalid tool call"))
		return
	}
	route, found := dependencies.Resolve(params.Name)
	if !found {
		write(writer, http.StatusNotFound, failure(rpc.ID, -32601, "tool not found"))
		return
	}
	timeout := dependencies.CallTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	if route.Timeout > 0 && route.Timeout < timeout {
		timeout = route.Timeout
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(request.Context()), timeout)
	defer cancel()
	outcome, err := dependencies.Execute.Execute(ctx, executor.Request{Route: route, Principal: principal, Arguments: params.Arguments, RequestID: rpc.ID, SessionID: session.ID, Deadline: time.Now().Add(timeout)}, nil)
	if err != nil {
		write(writer, http.StatusOK, failure(rpc.ID, -32603, "tool execution failed"))
		return
	}
	if outcome.ProtocolError != nil {
		write(writer, http.StatusOK, failure(rpc.ID, outcome.ProtocolError.Code, outcome.ProtocolError.Message))
		return
	}
	if outcome.InputRequired != nil {
		write(writer, http.StatusOK, failure(rpc.ID, -32004, "interactive input is unsupported"))
		return
	}
	if outcome.Complete == nil {
		write(writer, http.StatusOK, failure(rpc.ID, -32603, "invalid tool outcome"))
		return
	}
	write(writer, http.StatusOK, success(rpc.ID, map[string]any{"content": outcome.Complete.Content, "structuredContent": json.RawMessage(outcome.Complete.Structured), "isError": outcome.Complete.IsError}))
}

func principalFor(dependencies Dependencies, request *http.Request) (contracts.Principal, bool) {
	if dependencies.Principal != nil {
		return dependencies.Principal(request)
	}
	return edge.PrincipalFromContext(request.Context())
}
func success(id json.RawMessage, result any) rpcResponse {
	return rpcResponse{JSONRPC: "2.0", ID: id, Result: result}
}
func failure(id json.RawMessage, code int, message string) rpcResponse {
	return rpcResponse{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: message}}
}
func write(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}
