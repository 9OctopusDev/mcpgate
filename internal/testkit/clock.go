package testkit

import (
	"sync"
	"time"
)

type Clock struct {
	mu  sync.Mutex
	now time.Time
}

func NewClock(now time.Time) *Clock { return &Clock{now: now} }
func (clock *Clock) Now() time.Time { clock.mu.Lock(); defer clock.mu.Unlock(); return clock.now }
func (clock *Clock) Advance(delta time.Duration) time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	clock.now = clock.now.Add(delta)
	return clock.now
}
