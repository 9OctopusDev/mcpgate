package telemetry

import (
	"errors"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
)

var ErrLabelCardinality = errors.New("metric label cardinality exceeds configured catalog bounds")

type LabelBounds struct{ Servers, Tools, Upstreams int }
type Metrics struct {
	mu                        sync.Mutex
	bounds                    LabelBounds
	servers, tools, upstreams map[string]struct{}
	calls                     *prometheus.CounterVec
	duration                  *prometheus.HistogramVec
}

func NewMetrics(registerer prometheus.Registerer, bounds LabelBounds) (*Metrics, error) {
	if registerer == nil || bounds.Servers <= 0 || bounds.Tools <= 0 || bounds.Upstreams <= 0 {
		return nil, errors.New("invalid metric registry or label bounds")
	}
	labels := []string{"server", "tool", "upstream", "method", "outcome"}
	metrics := &Metrics{bounds: bounds, servers: make(map[string]struct{}), tools: make(map[string]struct{}), upstreams: make(map[string]struct{}), calls: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "mcpgate_tool_calls_total", Help: "Completed gateway tool calls."}, labels), duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "mcpgate_tool_call_duration_seconds", Help: "End-to-end gateway tool call duration.", Buckets: prometheus.DefBuckets}, labels)}
	if err := registerer.Register(metrics.calls); err != nil {
		return nil, err
	}
	if err := registerer.Register(metrics.duration); err != nil {
		return nil, err
	}
	return metrics, nil
}
func (metrics *Metrics) ObserveCall(server, tool, upstream, method, outcome string, seconds float64) error {
	if !allowedFixed(method, map[string]bool{"GET": true, "POST": true, "PUT": true, "PATCH": true, "DELETE": true, "MCP": true}) || !allowedFixed(outcome, map[string]bool{"complete": true, "business_error": true, "protocol_error": true, "canceled": true, "timeout": true, "unknown": true}) {
		return ErrLabelCardinality
	}
	metrics.mu.Lock()
	defer metrics.mu.Unlock()
	if !canAdmitLabel(metrics.servers, server, metrics.bounds.Servers) || !canAdmitLabel(metrics.tools, tool, metrics.bounds.Tools) || !canAdmitLabel(metrics.upstreams, upstream, metrics.bounds.Upstreams) {
		return ErrLabelCardinality
	}
	metrics.servers[server] = struct{}{}
	metrics.tools[tool] = struct{}{}
	metrics.upstreams[upstream] = struct{}{}
	metrics.calls.WithLabelValues(server, tool, upstream, method, outcome).Inc()
	metrics.duration.WithLabelValues(server, tool, upstream, method, outcome).Observe(seconds)
	return nil
}
func canAdmitLabel(values map[string]struct{}, value string, maximum int) bool {
	if value == "" {
		return false
	}
	if _, exists := values[value]; exists {
		return true
	}
	if len(values) >= maximum {
		return false
	}
	return true
}
func allowedFixed(value string, allowed map[string]bool) bool { return allowed[value] }
