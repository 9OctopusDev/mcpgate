package telemetry

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

func TestMetricsBoundLabelsAndRejectCardinalityOverflow(t *testing.T) {
	registry := prometheus.NewRegistry()
	metrics, err := NewMetrics(registry, LabelBounds{Servers: 2, Tools: 2, Upstreams: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := metrics.ObserveCall("server", "tool", "upstream", "POST", "complete", 0.01); err != nil {
		t.Fatal(err)
	}
	if err := metrics.ObserveCall("server", "tool", "another", "POST", "complete", 0.01); err == nil {
		t.Fatal("unbounded upstream label accepted")
	}
	families, err := registry.Gather()
	if err != nil || len(families) == 0 {
		t.Fatalf("families=%d err=%v", len(families), err)
	}
}
