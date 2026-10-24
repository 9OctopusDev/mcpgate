package benchmarks_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/9OctopusDev/mcpgate/internal/contracts"
	"github.com/9OctopusDev/mcpgate/internal/executor"
	"github.com/9OctopusDev/mcpgate/internal/protocol/modern"
)

const mockUpstreamLatency = 20 * time.Millisecond

type benchmarkExecutor struct{}

func (benchmarkExecutor) Execute(ctx context.Context, _ executor.Request, _ contracts.EventSink) (contracts.Outcome, error) {
	timer := time.NewTimer(mockUpstreamLatency)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return contracts.Outcome{}, ctx.Err()
	case <-timer.C:
		return contracts.Outcome{Complete: contracts.NewToolResult(json.RawMessage(`{"ok":true}`), nil, false)}, nil
	}
}

func BenchmarkGateway(b *testing.B) {
	routes := make(map[string]contracts.Route, 1000)
	for index := 0; index < 1000; index++ {
		name := fmt.Sprintf("tool.%04d", index)
		routes[name] = contracts.Route{Key: contracts.RouteKey{ServerID: "benchmark", PublicName: name}, NativeName: name}
	}
	handler := modern.Handler(modern.Dependencies{
		ServerID: "benchmark",
		Principal: func(*http.Request) (contracts.Principal, bool) {
			return contracts.Principal{PartitionDigest: "benchmark"}, true
		},
		Resolve: func(name string) (contracts.Route, bool) { route, ok := routes[name]; return route, ok },
		Execute: benchmarkExecutor{},
	})
	padding := strings.Repeat("x", 768)
	body := []byte(fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"tool.0000","arguments":{"payload":%q}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientInfo":{"name":"bench","version":"1"},"io.modelcontextprotocol/clientCapabilities":{}}}`, padding))
	if len(body) < 1024 || len(body) > 1152 {
		b.Fatalf("benchmark request size = %d bytes", len(body))
	}

	const concurrency = 100
	const bucketWidth = 100 * time.Microsecond
	var buckets [201]atomic.Uint64
	jobs := make(chan struct{})
	var workers sync.WaitGroup
	workers.Add(concurrency)
	b.ReportAllocs()
	b.ResetTimer()
	for range concurrency {
		go func() {
			defer workers.Done()
			for range jobs {
				started := time.Now()
				request := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
				request.Header.Set("MCP-Protocol-Version", modern.ProtocolVersion)
				request.Header.Set("MCP-Client-Name", "bench")
				request.Header.Set("MCP-Client-Version", "1")
				request.Header.Set("MCP-Name", "tool.0000")
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, request)
				if response.Code != http.StatusOK {
					b.Errorf("status=%d", response.Code)
				}
				added := time.Since(started) - mockUpstreamLatency
				if added < 0 {
					added = 0
				}
				bucket := int(added / bucketWidth)
				if bucket >= len(buckets) {
					bucket = len(buckets) - 1
				}
				buckets[bucket].Add(1)
			}
		}()
	}
	for range b.N {
		jobs <- struct{}{}
	}
	close(jobs)
	workers.Wait()
	b.StopTimer()

	target := uint64((b.N*95 + 99) / 100)
	var observed uint64
	var p95 time.Duration
	for index := range buckets {
		observed += buckets[index].Load()
		if observed >= target {
			p95 = time.Duration(index+1) * bucketWidth
			break
		}
	}
	b.ReportMetric(float64(p95.Nanoseconds()), "ns/gateway-p95")
	b.ReportMetric(concurrency, "concurrent")
	b.ReportMetric(float64(len(body)), "bytes/request")
	b.ReportMetric(float64(len(routes)), "tools")
}
