package conformance_test

import "testing"

func TestConformanceMatrix(t *testing.T) {
	type cell struct{ downstream, upstream, lifecycle, cancellation, session string }
	cells := []cell{
		{"modern", "rest", "discover", "request-context", "none"},
		{"modern", "modern-mcp", "discover", "request-context", "none"},
		{"modern", "legacy-mcp", "discover-then-initialize", "translated", "upstream-pinned"},
		{"legacy", "rest", "initialize", "session-request", "gateway"},
		{"legacy", "modern-mcp", "initialize", "translated", "gateway"},
		{"legacy", "legacy-mcp", "initialize", "session-request", "gateway-and-upstream"},
	}
	seen := make(map[string]bool)
	for _, cell := range cells {
		key := cell.downstream + "->" + cell.upstream
		if seen[key] {
			t.Fatalf("duplicate matrix cell %s", key)
		}
		seen[key] = true
		if cell.lifecycle == "" || cell.cancellation == "" || cell.session == "" {
			t.Fatalf("incomplete matrix cell %+v", cell)
		}
	}
	if len(seen) != 6 {
		t.Fatalf("matrix has %d cells", len(seen))
	}
}
