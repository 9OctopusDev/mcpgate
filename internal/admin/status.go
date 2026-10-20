package admin

import gatewayruntime "github.com/mcpgate/mcpgate/internal/runtime"

type StatusProvider interface {
	Status() gatewayruntime.Status
	Upstreams() any
	Routes() any
}
