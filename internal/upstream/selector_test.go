package upstream

import (
	"errors"
	"sync"
	"testing"
	"time"
)

type mutableClock struct {
	mu  sync.Mutex
	now time.Time
}

func (clock *mutableClock) Now() time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return clock.now
}
func (clock *mutableClock) Add(duration time.Duration) {
	clock.mu.Lock()
	clock.now = clock.now.Add(duration)
	clock.mu.Unlock()
}

func TestWeightedSelectionUsesSmoothDistribution(t *testing.T) {
	pool, err := NewPool([]EndpointConfig{
		{ID: "heavy", URL: "https://a.example", Weight: 5, Fingerprint: "same"},
		{ID: "light", URL: "https://b.example", Weight: 1, Fingerprint: "same"},
	}, PoolOptions{ExpectedFingerprint: "same"})
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for range 12 {
		endpoint, err := pool.Select("")
		if err != nil {
			t.Fatal(err)
		}
		counts[endpoint.ID]++
		pool.Observe(endpoint, ResultSuccess)
	}
	if counts["heavy"] != 10 || counts["light"] != 2 {
		t.Fatalf("unexpected weighted distribution: %#v", counts)
	}
}

func TestAffinitySticksUntilEndpointUnavailable(t *testing.T) {
	pool, err := NewPool([]EndpointConfig{
		{ID: "a", URL: "https://a.example", Weight: 1, Fingerprint: "same"},
		{ID: "b", URL: "https://b.example", Weight: 1, Fingerprint: "same"},
	}, PoolOptions{ExpectedFingerprint: "same"})
	if err != nil {
		t.Fatal(err)
	}
	first, err := pool.Select("session-1")
	if err != nil {
		t.Fatal(err)
	}
	pool.Observe(first, ResultSuccess)
	second, err := pool.Select("session-1")
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != first.ID {
		t.Fatalf("affinity moved: %s -> %s", first.ID, second.ID)
	}
	pool.Observe(second, ResultSuccess)
	pool.SetHealthy(first.ID, false)
	failover, err := pool.Select("session-1")
	if err != nil {
		t.Fatal(err)
	}
	if failover.ID == first.ID {
		t.Fatal("unhealthy affinity endpoint remained selected")
	}
	pool.Observe(failover, ResultSuccess)
}

func TestCircuitCountsAvailabilityFailuresButNotBusinessErrors(t *testing.T) {
	clock := &mutableClock{now: time.Unix(1000, 0)}
	pool, err := NewPool([]EndpointConfig{{ID: "a", URL: "https://a.example", Weight: 1, Fingerprint: "same"}}, PoolOptions{
		ExpectedFingerprint: "same", FailureThreshold: 2, OpenDuration: time.Minute, Now: clock.Now,
	})
	if err != nil {
		t.Fatal(err)
	}
	endpoint, _ := pool.Select("")
	pool.Observe(endpoint, ResultBusinessError)
	endpoint, err = pool.Select("")
	if err != nil {
		t.Fatal("business error opened circuit")
	}
	pool.Observe(endpoint, ResultAvailabilityFailure)
	endpoint, err = pool.Select("")
	if err != nil {
		t.Fatal("circuit opened below threshold")
	}
	pool.Observe(endpoint, ResultAvailabilityFailure)
	if _, err := pool.Select(""); !errors.Is(err, ErrNoHealthyEndpoint) {
		t.Fatalf("open circuit still selected: %v", err)
	}
	clock.Add(time.Minute)
	probe, err := pool.Select("")
	if err != nil {
		t.Fatalf("half-open probe unavailable: %v", err)
	}
	if _, err := pool.Select(""); !errors.Is(err, ErrNoHealthyEndpoint) {
		t.Fatalf("second half-open probe allowed: %v", err)
	}
	pool.Observe(probe, ResultSuccess)
	if _, err := pool.Select(""); err != nil {
		t.Fatalf("successful probe did not close circuit: %v", err)
	}
}

func TestWeightedPoolQuarantinesIncompatibleReplica(t *testing.T) {
	pool, err := NewPool([]EndpointConfig{
		{ID: "good", URL: "https://a.example", Weight: 1, Fingerprint: "expected"},
		{ID: "changed", URL: "https://b.example", Weight: 100, Fingerprint: "different"},
	}, PoolOptions{ExpectedFingerprint: "expected"})
	if err != nil {
		t.Fatal(err)
	}
	for range 10 {
		endpoint, err := pool.Select("")
		if err != nil {
			t.Fatal(err)
		}
		if endpoint.ID != "good" {
			t.Fatalf("incompatible endpoint selected: %+v", endpoint)
		}
		pool.Observe(endpoint, ResultSuccess)
	}
	if reason, ok := pool.QuarantineReason("changed"); !ok || reason == "" {
		t.Fatal("fingerprint mismatch was not recorded as quarantine")
	}
}
