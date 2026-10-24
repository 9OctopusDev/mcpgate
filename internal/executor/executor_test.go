package executor

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/9OctopusDev/mcpgate/internal/contracts"
)

type testAuthorizer struct {
	mu      sync.Mutex
	allowed bool
	epoch   uint64
}

func (a *testAuthorizer) Authorize(context.Context, contracts.Principal, contracts.Route) (contracts.Decision, error) {
	return contracts.Decision{Allowed: a.allowed, Epoch: a.epoch}, nil
}
func (a *testAuthorizer) RecheckAtDispatch(context.Context, contracts.Principal, contracts.Route, uint64) (contracts.Decision, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return contracts.Decision{Allowed: a.allowed, Epoch: a.epoch}, nil
}

type testAdapter struct {
	mu    sync.Mutex
	calls int
	err   error
}

func (a *testAdapter) Discover(context.Context, contracts.CredentialBinding) (contracts.Catalog, error) {
	return contracts.Catalog{}, nil
}
func (a *testAdapter) CallTool(context.Context, contracts.Call, contracts.EventSink) (contracts.Outcome, error) {
	a.mu.Lock()
	a.calls++
	a.mu.Unlock()
	if a.err != nil {
		return contracts.Outcome{}, a.err
	}
	return contracts.Outcome{Complete: contracts.NewToolResult(json.RawMessage(`{"ok":true}`), nil, false)}, nil
}
func (*testAdapter) Close(context.Context) error { return nil }

type auditRecorder struct {
	mu     sync.Mutex
	events []AuditEvent
}

func (r *auditRecorder) Record(_ context.Context, event AuditEvent) {
	r.mu.Lock()
	r.events = append(r.events, event)
	r.mu.Unlock()
}

func TestExecuteOrdersAuthorizationDispatchAndRelease(t *testing.T) {
	authorizer := &testAuthorizer{allowed: true, epoch: 4}
	adapter := &testAdapter{}
	audit := &auditRecorder{}
	executor, err := New(Dependencies{
		Authorizer: authorizer,
		Adapters:   StaticAdapters{"users": adapter},
		Audit:      audit,
	}, Limits{Active: 1, Queued: 1, QueueWait: time.Second, BufferBytes: 1024, PerPrincipalRate: 100, PerPrincipalBurst: 1})
	if err != nil {
		t.Fatal(err)
	}
	route := contracts.Route{Key: contracts.RouteKey{ServerID: "public", PublicName: "users.get"}, UpstreamID: "users", NativeName: "getUser", Behavior: contracts.BehaviorReadOnly, MaxAttempts: 1}
	outcome, err := executor.Execute(context.Background(), Request{Route: route, Principal: contracts.Principal{PartitionDigest: "partition"}, Arguments: json.RawMessage(`{"id":"1"}`)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Complete == nil || adapter.calls != 1 {
		t.Fatalf("outcome=%+v calls=%d", outcome, adapter.calls)
	}
	if len(audit.events) != 2 || audit.events[0].Phase != "dispatch" || audit.events[1].Phase != "complete" {
		t.Fatalf("audit events=%+v", audit.events)
	}
	if got := executor.Active(); got != 0 {
		t.Fatalf("active permits leaked: %d", got)
	}
}

func TestDispatchRecheckPreventsCall(t *testing.T) {
	authorizer := &testAuthorizer{allowed: false, epoch: 2}
	adapter := &testAdapter{}
	executor, err := New(Dependencies{Authorizer: authorizer, Adapters: StaticAdapters{"users": adapter}}, Limits{Active: 1, Queued: 1, QueueWait: time.Second, BufferBytes: 1024, PerPrincipalRate: 10, PerPrincipalBurst: 1})
	if err != nil {
		t.Fatal(err)
	}
	_, err = executor.Execute(context.Background(), Request{Route: contracts.Route{Key: contracts.RouteKey{ServerID: "s", PublicName: "t"}, UpstreamID: "users"}, Principal: contracts.Principal{PartitionDigest: "p"}}, nil)
	if !errors.Is(err, ErrDenied) || adapter.calls != 0 {
		t.Fatalf("err=%v calls=%d", err, adapter.calls)
	}
}

func TestCancellationIsScopedAndMarksDispatchedUnknown(t *testing.T) {
	registry := NewCorrelations()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	key := CorrelationKey{Principal: "p", Session: "s", Request: "7"}
	registry.Track(key, "execution", cancel)
	defer registry.Untrack(key, "execution")
	if registry.Cancel(CorrelationKey{Principal: "other", Session: "s", Request: "7"}) {
		t.Fatal("cross-principal cancellation succeeded")
	}
	if !registry.Cancel(key) {
		t.Fatal("scoped cancellation was not found")
	}
	select {
	case <-ctx.Done():
	default:
		t.Fatal("context was not canceled")
	}
}
