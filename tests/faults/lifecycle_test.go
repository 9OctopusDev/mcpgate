package faults_test

import (
	"context"
	"errors"
	"testing"
	"time"

	gatewayruntime "github.com/mcpgate/mcpgate/internal/runtime"
)

type lifecycleBuilder struct {
	build func(context.Context) (gatewayruntime.Candidate, error)
}

func (builder lifecycleBuilder) Build(ctx context.Context) (gatewayruntime.Candidate, error) {
	return builder.build(ctx)
}

func TestReloadFailurePreservesActiveGeneration(t *testing.T) {
	store := gatewayruntime.NewStore(gatewayruntime.NewGeneration(gatewayruntime.GenerationSpec{Revision: "stable", DrainTimeout: time.Second}))
	coordinator, err := gatewayruntime.NewCoordinator(gatewayruntime.CoordinatorOptions{Store: store, Builder: lifecycleBuilder{build: func(context.Context) (gatewayruntime.Candidate, error) {
		return gatewayruntime.Candidate{}, errors.New("candidate failed")
	}}, InitialTopology: "same", ShutdownTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if coordinator.Reload(context.Background()) == nil {
		t.Fatal("reload succeeded")
	}
	lease, err := store.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	if lease.Generation().Revision() != "stable" {
		t.Fatal("failed reload replaced active generation")
	}
}

func TestShutdownLeaseDeadlineIsBounded(t *testing.T) {
	closed := make(chan struct{})
	resource := gatewayruntime.NewSharedResource(func(context.Context) error { close(closed); return nil })
	store := gatewayruntime.NewStore(gatewayruntime.NewGeneration(gatewayruntime.GenerationSpec{Revision: "active", DrainTimeout: time.Hour, Resources: []*gatewayruntime.SharedResource{resource}}))
	lease, _ := store.Acquire()
	defer lease.Release()
	coordinator, _ := gatewayruntime.NewCoordinator(gatewayruntime.CoordinatorOptions{Store: store, Builder: lifecycleBuilder{build: func(context.Context) (gatewayruntime.Candidate, error) { return gatewayruntime.Candidate{}, nil }}, ShutdownTimeout: 20 * time.Millisecond})
	started := time.Now()
	err := coordinator.Shutdown(context.Background())
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err=%v", err)
	}
	if time.Since(started) > time.Second {
		t.Fatal("shutdown was not bounded")
	}
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("resource not forcibly retired")
	}
}

func TestRevocationOverlayPublishesAtomically(t *testing.T) {
	overlay := gatewayruntime.NewRevocations()
	epoch := overlay.Publish([]string{"principal"}, nil, []string{"upstream"})
	snapshot := overlay.Snapshot()
	if snapshot.Epoch != epoch {
		t.Fatal("epoch mismatch")
	}
	if _, ok := snapshot.Principals["principal"]; !ok {
		t.Fatal("principal revocation absent")
	}
	delete(snapshot.Principals, "principal")
	if _, ok := overlay.Snapshot().Principals["principal"]; !ok {
		t.Fatal("snapshot mutation escaped")
	}
}
