package admin

import gatewayruntime "github.com/9OctopusDev/mcpgate/internal/runtime"

type StatusProvider interface {
	Status() gatewayruntime.Status
	Upstreams() any
	Routes() any
}
