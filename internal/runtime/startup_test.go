package runtime

import (
	"context"
	"errors"
	"testing"
	"time"
)

type buildFunc func(context.Context) (Candidate, error)

func (fn buildFunc) Build(ctx context.Context) (Candidate, error) { return fn(ctx) }

func TestStartupPublishesOnlyAfterActivationAndTracksReadiness(t *testing.T) {
	store := NewStore(nil)
	activated := false
	coordinator, err := NewCoordinator(CoordinatorOptions{Store: store, Builder: buildFunc(func(context.Context) (Candidate, error) {
		return Candidate{Generation: NewGeneration(GenerationSpec{Revision: "r1", DrainTimeout: time.Second}), ListenerTopology: "data=:8080"}, nil
	}), Activate: func(context.Context, Candidate) error { activated = true; return nil }, ShutdownTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if coordinator.Status().Ready {
		t.Fatal("ready before startup")
	}
	if err := coordinator.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !activated || !coordinator.Status().Ready {
		t.Fatalf("activated=%v status=%+v", activated, coordinator.Status())
	}
	lease, err := store.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	if lease.Generation().Revision() != "r1" {
		t.Fatal("wrong generation")
	}
}

func TestStartupFailureLeavesNotReady(t *testing.T) {
	coordinator, _ := NewCoordinator(CoordinatorOptions{Store: NewStore(nil), Builder: buildFunc(func(context.Context) (Candidate, error) { return Candidate{}, errors.New("required discovery failed") }), ShutdownTimeout: time.Second})
	if coordinator.Start(context.Background()) == nil {
		t.Fatal("startup succeeded")
	}
	status := coordinator.Status()
	if status.Ready || status.Reason == "" {
		t.Fatalf("status=%+v", status)
	}
}
