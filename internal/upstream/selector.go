package upstream

import (
	"errors"
	"fmt"
	"net/url"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

var ErrNoHealthyEndpoint = errors.New("no healthy compatible upstream endpoint")

type EndpointConfig struct {
	ID          string
	URL         string
	Weight      int
	Fingerprint string
}

type PoolOptions struct {
	ExpectedFingerprint string
	FailureThreshold    int
	OpenDuration        time.Duration
	Now                 func() time.Time
}

type Endpoint struct {
	ID          string
	URL         string
	Weight      int
	Fingerprint string

	current    int
	inflight   atomic.Int64
	healthy    bool
	quarantine string
	circuit    circuit
}

func (endpoint *Endpoint) Inflight() int64 { return endpoint.inflight.Load() }

type Pool struct {
	mu               sync.Mutex
	endpoints        []*Endpoint
	byID             map[string]*Endpoint
	affinity         map[string]string
	failureThreshold int
	openDuration     time.Duration
	now              func() time.Time
}

func NewPool(configurations []EndpointConfig, options PoolOptions) (*Pool, error) {
	if len(configurations) == 0 {
		return nil, ErrNoHealthyEndpoint
	}
	if options.FailureThreshold <= 0 {
		options.FailureThreshold = 3
	}
	if options.OpenDuration <= 0 {
		options.OpenDuration = 30 * time.Second
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	expected := options.ExpectedFingerprint
	if expected == "" {
		expected = configurations[0].Fingerprint
	}
	pool := &Pool{
		byID: make(map[string]*Endpoint, len(configurations)), affinity: make(map[string]string),
		failureThreshold: options.FailureThreshold, openDuration: options.OpenDuration, now: options.Now,
	}
	for _, configuration := range configurations {
		if configuration.ID == "" || configuration.Weight <= 0 {
			return nil, fmt.Errorf("invalid endpoint configuration")
		}
		if _, exists := pool.byID[configuration.ID]; exists {
			return nil, fmt.Errorf("duplicate endpoint id")
		}
		parsed, err := url.Parse(configuration.URL)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return nil, fmt.Errorf("invalid endpoint URL")
		}
		endpoint := &Endpoint{
			ID: configuration.ID, URL: configuration.URL, Weight: configuration.Weight, Fingerprint: configuration.Fingerprint,
			healthy: true, circuit: newCircuit(),
		}
		if configuration.Fingerprint != expected {
			endpoint.quarantine = "capability fingerprint mismatch"
		}
		pool.endpoints = append(pool.endpoints, endpoint)
		pool.byID[endpoint.ID] = endpoint
	}
	sort.Slice(pool.endpoints, func(i, j int) bool { return pool.endpoints[i].ID < pool.endpoints[j].ID })
	return pool, nil
}

func (pool *Pool) Select(affinityKey string) (*Endpoint, error) {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	now := pool.now()
	if affinityKey != "" {
		if endpoint := pool.byID[pool.affinity[affinityKey]]; pool.selectable(endpoint, now) {
			endpoint.circuit.selected(now, pool.openDuration)
			endpoint.inflight.Add(1)
			return endpoint, nil
		}
	}
	var selected *Endpoint
	totalWeight := 0
	for _, endpoint := range pool.endpoints {
		if !pool.selectable(endpoint, now) {
			continue
		}
		endpoint.current += endpoint.Weight
		totalWeight += endpoint.Weight
		if selected == nil || endpoint.current > selected.current {
			selected = endpoint
		}
	}
	if selected == nil {
		return nil, ErrNoHealthyEndpoint
	}
	selected.current -= totalWeight
	selected.circuit.selected(now, pool.openDuration)
	selected.inflight.Add(1)
	if affinityKey != "" {
		pool.affinity[affinityKey] = selected.ID
	}
	return selected, nil
}

func (pool *Pool) selectable(endpoint *Endpoint, now time.Time) bool {
	return endpoint != nil && endpoint.healthy && endpoint.quarantine == "" && endpoint.circuit.eligible(now, pool.openDuration)
}

func (pool *Pool) Observe(endpoint *Endpoint, result ResultKind) {
	if endpoint == nil {
		return
	}
	pool.mu.Lock()
	defer pool.mu.Unlock()
	owned := pool.byID[endpoint.ID]
	if owned == nil || owned != endpoint {
		return
	}
	if endpoint.inflight.Load() > 0 {
		endpoint.inflight.Add(-1)
	}
	switch result {
	case ResultAvailabilityFailure:
		endpoint.circuit.failure(pool.now(), pool.failureThreshold)
	default:
		endpoint.circuit.success()
	}
}

func (pool *Pool) CircuitState(endpointID string) (CircuitState, bool) {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	endpoint := pool.byID[endpointID]
	if endpoint == nil {
		return "", false
	}
	return endpoint.circuit.state, true
}
