package telemetry

import (
	"net/http"
	"testing"
)

func TestForwardTraceHeadersDropsBaggageAndCredentials(t *testing.T) {
	source := make(http.Header)
	source.Set("Traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	source.Set("Tracestate", "vendor=value")
	source.Set("Baggage", "secret=value")
	source.Set("Authorization", "Bearer secret")
	forwarded := ForwardTraceHeaders(source)
	if forwarded.Get("Traceparent") == "" || forwarded.Get("Tracestate") == "" {
		t.Fatal("trace context missing")
	}
	if forwarded.Get("Baggage") != "" || forwarded.Get("Authorization") != "" {
		t.Fatalf("unsafe headers forwarded: %v", forwarded)
	}
}
