package executor

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

var (
	ErrOverloaded  = errors.New("gateway admission capacity exceeded")
	ErrRateLimited = errors.New("principal rate limit exceeded")
)

type Limits struct {
	Active            int
	Queued            int
	QueueWait         time.Duration
	BufferBytes       int64
	PerPrincipalRate  float64
	PerPrincipalBurst int
}

type bucket struct {
	tokens float64
	last   time.Time
}

type admission struct {
	active   chan struct{}
	queued   chan struct{}
	buffer   *bufferBudget
	rate     float64
	burst    int
	mu       sync.Mutex
	buckets  map[string]*bucket
	inflight atomic.Int64
}

func newAdmission(limits Limits) *admission {
	return &admission{
		active: make(chan struct{}, limits.Active), queued: make(chan struct{}, limits.Queued),
		buffer: newBufferBudget(limits.BufferBytes), rate: limits.PerPrincipalRate, burst: limits.PerPrincipalBurst,
		buckets: make(map[string]*bucket),
	}
}

func (a *admission) allowPrincipal(key string, now time.Time) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	b := a.buckets[key]
	if b == nil {
		b = &bucket{tokens: float64(a.burst), last: now}
		a.buckets[key] = b
	}
	elapsed := now.Sub(b.last).Seconds()
	if elapsed > 0 {
		b.tokens += elapsed * a.rate
		if b.tokens > float64(a.burst) {
			b.tokens = float64(a.burst)
		}
		b.last = now
	}
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

func (a *admission) acquire(ctx context.Context, queueWait time.Duration) (func(), error) {
	select {
	case a.queued <- struct{}{}:
	default:
		return nil, ErrOverloaded
	}
	queued := true
	releaseQueue := func() {
		if queued {
			<-a.queued
			queued = false
		}
	}
	timer := time.NewTimer(queueWait)
	defer timer.Stop()
	select {
	case a.active <- struct{}{}:
		releaseQueue()
		a.inflight.Add(1)
		var once sync.Once
		return func() { once.Do(func() { <-a.active; a.inflight.Add(-1) }) }, nil
	case <-ctx.Done():
		releaseQueue()
		return nil, ctx.Err()
	case <-timer.C:
		releaseQueue()
		return nil, ErrOverloaded
	}
}
