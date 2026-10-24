package runtime

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/9OctopusDev/mcpgate/internal/contracts"
)

type VirtualServer struct {
	ID             string
	Path           string
	ResourceURI    string
	Authentication string
	Protocols      []string
	RouteKeys      []contracts.RouteKey
}

type GenerationSpec struct {
	Revision     string
	Routes       map[contracts.RouteKey]contracts.Route
	Servers      map[string]VirtualServer
	DrainTimeout time.Duration
	Resources    []*SharedResource
}

type Generation struct {
	revision     string
	routes       map[contracts.RouteKey]contracts.Route
	servers      map[string]VirtualServer
	drainTimeout time.Duration
	resources    []*SharedResource

	context context.Context
	cancel  context.CancelFunc
	once    sync.Once
}

func NewGeneration(spec GenerationSpec) *Generation {
	ctx, cancel := context.WithCancel(context.Background())
	generation := &Generation{
		revision: spec.Revision, routes: cloneRoutes(spec.Routes), servers: cloneServers(spec.Servers),
		drainTimeout: spec.DrainTimeout, resources: append([]*SharedResource(nil), spec.Resources...),
		context: ctx, cancel: cancel,
	}
	for _, resource := range generation.resources {
		resource.retain()
	}
	return generation
}

func (generation *Generation) Revision() string { return generation.revision }

func (generation *Generation) Context() context.Context { return generation.context }

func (generation *Generation) DrainTimeout() time.Duration { return generation.drainTimeout }

func (generation *Generation) Route(key contracts.RouteKey) (contracts.Route, bool) {
	route, ok := generation.routes[key]
	return cloneRoute(route), ok
}

func (generation *Generation) Server(id string) (VirtualServer, bool) {
	server, ok := generation.servers[id]
	if ok {
		server.Protocols = append([]string(nil), server.Protocols...)
		server.RouteKeys = append([]contracts.RouteKey(nil), server.RouteKeys...)
	}
	return server, ok
}

func (generation *Generation) close(ctx context.Context) {
	generation.once.Do(func() {
		generation.cancel()
		for _, resource := range generation.resources {
			resource.release(ctx)
		}
	})
}

func cloneRoutes(source map[contracts.RouteKey]contracts.Route) map[contracts.RouteKey]contracts.Route {
	result := make(map[contracts.RouteKey]contracts.Route, len(source))
	for key, route := range source {
		result[key] = cloneRoute(route)
	}
	return result
}

func cloneRoute(route contracts.Route) contracts.Route {
	route.Contract.InputSchema = append(json.RawMessage(nil), route.Contract.InputSchema...)
	route.Contract.OutputSchema = append(json.RawMessage(nil), route.Contract.OutputSchema...)
	route.Contract.RequiredFeatures = append([]string(nil), route.Contract.RequiredFeatures...)
	return route
}

func cloneServers(source map[string]VirtualServer) map[string]VirtualServer {
	result := make(map[string]VirtualServer, len(source))
	for id, server := range source {
		server.Protocols = append([]string(nil), server.Protocols...)
		server.RouteKeys = append([]contracts.RouteKey(nil), server.RouteKeys...)
		result[id] = server
	}
	return result
}
