package faults_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/mcpgate/mcpgate/internal/contracts"
	"github.com/mcpgate/mcpgate/internal/executor"
)

type allowAuthorizer struct{}

func (allowAuthorizer) Authorize(context.Context, contracts.Principal, contracts.Route) (contracts.Decision, error) {
	return contracts.Decision{Allowed: true, Epoch: 1}, nil
}
func (allowAuthorizer) RecheckAtDispatch(context.Context, contracts.Principal, contracts.Route, uint64) (contracts.Decision, error) {
	return contracts.Decision{Allowed: true, Epoch: 1}, nil
}

type blockingAdapter struct {
	started chan struct{}
	release chan struct{}
}

func (*blockingAdapter) Discover(context.Context, contracts.CredentialBinding) (contracts.Catalog, error) {
	return contracts.Catalog{}, nil
}
func (adapter *blockingAdapter) CallTool(ctx context.Context, _ contracts.Call, _ contracts.EventSink) (contracts.Outcome, error) {
	select {
	case adapter.started <- struct{}{}:
	default:
		{
		}
	}
	select {
	case <-adapter.release:
		return contracts.Outcome{Complete: contracts.NewToolResult(json.RawMessage(`{}`), nil, false)}, nil
	case <-ctx.Done():
		return contracts.Outcome{}, ctx.Err()
	}
}
func (*blockingAdapter) Close(context.Context) error { return nil }

func TestMemoryBufferBudgetRejectsConcurrentNearLimitResponses(t *testing.T) {
	adapter := &blockingAdapter{started: make(chan struct{}, 1), release: make(chan struct{})}
	execution, err := executor.New(executor.Dependencies{Authorizer: allowAuthorizer{}, Adapters: executor.StaticAdapters{"u": adapter}}, executor.Limits{Active: 2, Queued: 2, QueueWait: time.Second, BufferBytes: 1024, PerPrincipalRate: 100, PerPrincipalBurst: 10})
	if err != nil {
		t.Fatal(err)
	}
	route := contracts.Route{Key: contracts.RouteKey{ServerID: "s", PublicName: "t"}, UpstreamID: "u", MaxAttempts: 1}
	first := make(chan error, 1)
	go func() {
		_, err := execution.Execute(context.Background(), executor.Request{Route: route, Principal: contracts.Principal{PartitionDigest: "p1"}, BufferBytes: 900}, nil)
		first <- err
	}()
	<-adapter.started
	_, err = execution.Execute(context.Background(), executor.Request{Route: route, Principal: contracts.Principal{PartitionDigest: "p2"}, BufferBytes: 900}, nil)
	if !errors.Is(err, executor.ErrBufferBudget) {
		t.Fatalf("err=%v", err)
	}
	close(adapter.release)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
}
