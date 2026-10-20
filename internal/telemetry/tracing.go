package telemetry

import (
	"context"
	"net/http"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type Tracer struct{ tracer trace.Tracer }

func NewTracer(provider trace.TracerProvider) *Tracer {
	if provider == nil {
		provider = otel.GetTracerProvider()
	}
	return &Tracer{tracer: provider.Tracer("mcpgate")}
}
func (tracer *Tracer) Start(ctx context.Context, stage string, fields map[string]string) (context.Context, trace.Span) {
	if !map[string]bool{"validation": true, "queueing": true, "credentials": true, "upstream": true, "normalization": true}[stage] {
		stage = "operation"
	}
	attributes := make([]attribute.KeyValue, 0, len(fields))
	for _, key := range []string{"server", "tool", "upstream", "method", "outcome", "reason", "state"} {
		if value := fields[key]; value != "" {
			attributes = append(attributes, attribute.String(key, value))
		}
	}
	return tracer.tracer.Start(ctx, "mcpgate."+stage, trace.WithAttributes(attributes...))
}
func ForwardTraceHeaders(source http.Header) http.Header {
	result := make(http.Header)
	for _, key := range []string{"Traceparent", "Tracestate"} {
		for _, value := range source.Values(key) {
			if len(value) <= 512 {
				result.Add(key, value)
			}
		}
	}
	return result
}
