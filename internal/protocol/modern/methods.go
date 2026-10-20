package modern

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/mcpgate/mcpgate/internal/contracts"
	"github.com/mcpgate/mcpgate/internal/edge"
	"github.com/mcpgate/mcpgate/internal/executor"
)

type Executor interface {
	Execute(context.Context, executor.Request, contracts.EventSink) (contracts.Outcome, error)
}
type CatalogPage struct {
	Tools      []contracts.ToolContract `json:"tools"`
	NextCursor string                   `json:"nextCursor,omitempty"`
	TTLMillis  int                      `json:"ttlMs"`
	CacheScope string                   `json:"cacheScope"`
}

type Dependencies struct {
	ServerID  string
	Revision  string
	Resolve   func(string) (contracts.Route, bool)
	List      func(context.Context, contracts.Principal, string) (CatalogPage, error)
	Execute   Executor
	Cancel    func(executor.CorrelationKey) bool
	Principal func(*http.Request) (contracts.Principal, bool)
}

func principalFor(dependencies Dependencies, request *http.Request) (contracts.Principal, bool) {
	if dependencies.Principal != nil {
		return dependencies.Principal(request)
	}
	return edge.PrincipalFromContext(request.Context())
}

func callTool(ctx context.Context, dependencies Dependencies, principal contracts.Principal, request wireRequest) (contracts.Outcome, error) {
	var parameters struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(request.Params, &parameters); err != nil || parameters.Name == "" {
		return contracts.Outcome{ProtocolError: &contracts.ProtocolError{Code: -32602, Message: "invalid tool call parameters"}}, nil
	}
	route, found := dependencies.Resolve(parameters.Name)
	if !found {
		return contracts.Outcome{ProtocolError: &contracts.ProtocolError{Code: -32601, Message: "tool not found"}}, nil
	}
	return dependencies.Execute.Execute(ctx, executor.Request{Route: route, Principal: principal, Arguments: parameters.Arguments, RequestID: request.ID}, nil)
}
