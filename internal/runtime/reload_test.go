package runtime

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestReloadIsTransactionalAndDetectsRestartRequired(t *testing.T) {
	store := NewStore(NewGeneration(GenerationSpec{Revision: "old", DrainTimeout: time.Second}))
	candidate := Candidate{Generation: NewGeneration(GenerationSpec{Revision: "new", DrainTimeout: time.Second}), ListenerTopology: "data=:9090"}
	coordinator, _ := NewCoordinator(CoordinatorOptions{Store: store, Builder: buildFunc(func(context.Context) (Candidate, error) { return candidate, nil }), InitialTopology: "data=:8080", ShutdownTimeout: time.Second})
	if err := coordinator.Reload(context.Background()); !errors.Is(err, ErrRestartRequired) {
		t.Fatalf("err=%v", err)
	}
	lease, _ := store.Acquire()
	defer lease.Release()
	if lease.Generation().Revision() != "old" {
		t.Fatal("restart-required reload replaced generation")
	}
}

func TestReloadOverlappingLeaseRetainsOldGeneration(t *testing.T) {
	closed := make(chan struct{})
	resource := NewSharedResource(func(context.Context) error { close(closed); return nil })
	store := NewStore(NewGeneration(GenerationSpec{Revision: "old", DrainTimeout: time.Second, Resources: []*SharedResource{resource}}))
	old, _ := store.Acquire()
	coordinator, _ := NewCoordinator(CoordinatorOptions{Store: store, Builder: buildFunc(func(context.Context) (Candidate, error) {
		return Candidate{Generation: NewGeneration(GenerationSpec{Revision: "new", DrainTimeout: time.Second}), ListenerTopology: "same"}, nil
	}), InitialTopology: "same", ShutdownTimeout: time.Second})
	if err := coordinator.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-closed:
		t.Fatal("old generation closed with active lease")
	default:
	}
	old.Release()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("old generation not retired")
	}
}
