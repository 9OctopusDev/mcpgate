package runtime

import (
	"context"
	"testing"
	"time"
)

func TestShutdownStopsAdmissionAndRetiresGeneration(t *testing.T) {
	closed := make(chan struct{})
	resource := NewSharedResource(func(context.Context) error { close(closed); return nil })
	store := NewStore(NewGeneration(GenerationSpec{Revision: "r", DrainTimeout: time.Second, Resources: []*SharedResource{resource}}))
	stopped := false
	coordinator, _ := NewCoordinator(CoordinatorOptions{Store: store, Builder: buildFunc(func(context.Context) (Candidate, error) { return Candidate{}, nil }), StopAdmission: func() { stopped = true }, ShutdownTimeout: time.Second})
	if err := coordinator.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !stopped || coordinator.Status().Ready {
		t.Fatalf("stopped=%v status=%+v", stopped, coordinator.Status())
	}
	if _, err := store.Acquire(); err == nil {
		t.Fatal("acquired after shutdown")
	}
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("generation not retired")
	}
}
