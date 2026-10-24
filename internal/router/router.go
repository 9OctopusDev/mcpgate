package router

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/9OctopusDev/mcpgate/internal/contracts"
)

var ErrUnknownTool = errors.New("unknown tool")

type AuditCause string

const (
	CauseAbsent      AuditCause = "absent"
	CauseDenied      AuditCause = "denied"
	CausePolicyError AuditCause = "policy_error"
)

type ResolutionFailure struct {
	Public error
	Cause  AuditCause
	Detail string
}

type Server struct {
	ID   string
	Path string
}

type Router struct {
	hosts  map[string]struct{}
	paths  map[string]string
	routes map[contracts.RouteKey]contracts.Route
}

func New(hosts []string, servers []Server, routes []contracts.Route) (*Router, error) {
	compiled := &Router{hosts: make(map[string]struct{}, len(hosts)), paths: make(map[string]string, len(servers)), routes: make(map[contracts.RouteKey]contracts.Route, len(routes))}
	for _, host := range hosts {
		if host == "" {
			return nil, fmt.Errorf("allowed host is empty")
		}
		compiled.hosts[strings.ToLower(host)] = struct{}{}
	}
	for _, server := range servers {
		if server.ID == "" || server.Path == "" || server.Path[0] != '/' {
			return nil, fmt.Errorf("invalid server route")
		}
		if _, exists := compiled.paths[server.Path]; exists {
			return nil, fmt.Errorf("duplicate server path")
		}
		compiled.paths[server.Path] = server.ID
	}
	for _, route := range routes {
		if _, exists := compiled.routes[route.Key]; exists {
			return nil, fmt.Errorf("duplicate public route")
		}
		compiled.routes[route.Key] = route
	}
	return compiled, nil
}

func (router *Router) Resolve(host, path, publicName string) (contracts.Route, bool) {
	if router == nil {
		return contracts.Route{}, false
	}
	if _, ok := router.hosts[strings.ToLower(host)]; !ok {
		return contracts.Route{}, false
	}
	serverID, ok := router.paths[path]
	if !ok {
		return contracts.Route{}, false
	}
	route, ok := router.routes[contracts.RouteKey{ServerID: serverID, PublicName: publicName}]
	return route, ok
}

func (router *Router) ResolveAuthorized(ctx context.Context, host, path, publicName string, principal contracts.Principal, authorizer contracts.Authorizer) (contracts.Route, ResolutionFailure) {
	route, found := router.Resolve(host, path, publicName)
	if !found {
		return contracts.Route{}, ResolutionFailure{Public: ErrUnknownTool, Cause: CauseAbsent, Detail: "route was not present"}
	}
	decision, err := authorizer.Authorize(ctx, principal, route)
	if err != nil {
		return contracts.Route{}, ResolutionFailure{Public: ErrUnknownTool, Cause: CausePolicyError, Detail: err.Error()}
	}
	if !decision.Allowed {
		return contracts.Route{}, ResolutionFailure{Public: ErrUnknownTool, Cause: CauseDenied, Detail: decision.Reason}
	}
	return route, ResolutionFailure{}
}
