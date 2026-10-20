package runtime

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mcpgate/mcpgate/internal/contracts"
)

func testGeneration(revision string, drain time.Duration, resources ...*SharedResource) *Generation {
	key := contracts.RouteKey{ServerID: "server", PublicName: "tool"}
	return NewGeneration(GenerationSpec{
		Revision: revision, DrainTimeout: drain, Resources: resources,
		Routes: map[contracts.RouteKey]contracts.Route{key: {Key: key, NativeName: revision}},
	})
}

func TestLeasePublishHonorsLongHeldLeaseAndDoubleRelease(t *testing.T) {
	var closed atomic.Int32
	resource := NewSharedResource(func(context.Context) error { closed.Add(1); return nil })
	first := testGeneration("first", time.Hour, resource)
	store := NewStore(first)
	lease, err := store.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	second := testGeneration("second", time.Hour)
	if err := store.Publish(context.Background(), second); err != nil {
		t.Fatal(err)
	}
	if closed.Load() != 0 || first.Context().Err() != nil {
		t.Fatal("retired generation closed while a lease remained active")
	}
	if lease.Generation().Revision() != "first" {
		t.Fatal("lease changed generation after publication")
	}
	lease.Release()
	lease.Release()
	if closed.Load() != 1 || !errors.Is(first.Context().Err(), context.Canceled) {
		t.Fatalf("retired generation was not closed exactly once: closes=%d context=%v", closed.Load(), first.Context().Err())
	}
}

func TestPublishDisposesFailedCandidate(t *testing.T) {
	var closed atomic.Int32
	resource := NewSharedResource(func(context.Context) error { closed.Add(1); return nil })
	candidate := testGeneration("candidate", time.Second, resource)
	store := NewStore(testGeneration("active", time.Second))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := store.Publish(ctx, candidate); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancelled publish, got %v", err)
	}
	if closed.Load() != 1 || !resource.Closed() {
		t.Fatalf("failed candidate resources were not disposed: closes=%d refs=%d", closed.Load(), resource.RefCount())
	}
	lease, _ := store.Acquire()
	defer lease.Release()
	if lease.Generation().Revision() != "active" {
		t.Fatal("failed candidate replaced active generation")
	}
}

func TestRetireSharedResourcesAfterLastGeneration(t *testing.T) {
	var closed atomic.Int32
	resource := NewSharedResource(func(context.Context) error { closed.Add(1); return nil })
	first := testGeneration("first", time.Second, resource)
	second := testGeneration("second", time.Second, resource)
	if resource.RefCount() != 2 {
		t.Fatalf("candidate generations did not retain shared resource: %d", resource.RefCount())
	}
	store := NewStore(first)
	if err := store.Publish(context.Background(), second); err != nil {
		t.Fatal(err)
	}
	if resource.RefCount() != 1 || resource.Closed() {
		t.Fatalf("shared resource closed while active generation uses it: refs=%d", resource.RefCount())
	}
	if err := store.Publish(context.Background(), testGeneration("third", time.Second)); err != nil {
		t.Fatal(err)
	}
	if resource.RefCount() != 0 || closed.Load() != 1 {
		t.Fatalf("shared resource was not closed after last generation: refs=%d closes=%d", resource.RefCount(), closed.Load())
	}
}

func TestRetireDrainDeadlineCancelsLongLease(t *testing.T) {
	closed := make(chan struct{})
	resource := NewSharedResource(func(context.Context) error { close(closed); return nil })
	first := testGeneration("first", 20*time.Millisecond, resource)
	store := NewStore(first)
	lease, err := store.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Publish(context.Background(), testGeneration("second", time.Second)); err != nil {
		t.Fatal(err)
	}
	select {
	case <-first.Context().Done():
	case <-time.After(time.Second):
		t.Fatal("retired generation was not cancelled at drain deadline")
	}
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("resource was not closed at drain deadline")
	}
	lease.Release()
}

func TestAcquirePublishRaceNeverMixesGenerations(t *testing.T) {
	store := NewStore(testGeneration("0", time.Hour))
	const iterations = 2000
	var wait sync.WaitGroup
	errorsSeen := make(chan error, iterations)
	wait.Add(2)
	go func() {
		defer wait.Done()
		for index := 1; index <= iterations; index++ {
			if err := store.Publish(context.Background(), testGeneration(time.Duration(index).String(), time.Hour)); err != nil {
				errorsSeen <- err
				return
			}
		}
	}()
	go func() {
		defer wait.Done()
		key := contracts.RouteKey{ServerID: "server", PublicName: "tool"}
		for index := 0; index < iterations; index++ {
			lease, err := store.Acquire()
			if err != nil {
				errorsSeen <- err
				return
			}
			generation := lease.Generation()
			route, ok := generation.Route(key)
			if !ok || route.NativeName != generation.Revision() || generation.Context().Err() != nil {
				errorsSeen <- errors.New("mixed or prematurely closed generation")
				lease.Release()
				return
			}
			lease.Release()
		}
	}()
	wait.Wait()
	close(errorsSeen)
	for err := range errorsSeen {
		t.Fatal(err)
	}
}
