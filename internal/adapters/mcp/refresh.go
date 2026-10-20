package mcp

import (
	"context"
	"sync"
	"time"
)

type Quarantine interface{ Quarantine(upstream, reason string) }

type Refresher struct {
	Adapter    *Adapter
	Upstream   string
	Interval   time.Duration
	MaxStale   time.Duration
	Quarantine Quarantine
	mu         sync.Mutex
	accepted   string
	lastValid  time.Time
}

func (refresher *Refresher) Check(ctx context.Context, now time.Time) error {
	result, err := refresher.Adapter.DetectAndDiscover(ctx)
	refresher.mu.Lock()
	defer refresher.mu.Unlock()
	if err != nil {
		if !refresher.lastValid.IsZero() && now.Sub(refresher.lastValid) > refresher.MaxStale && refresher.Quarantine != nil {
			refresher.Quarantine.Quarantine(refresher.Upstream, "discovery stale")
		}
		return err
	}
	if refresher.accepted == "" {
		refresher.accepted = result.Fingerprint
	}
	if result.Fingerprint != refresher.accepted && refresher.Quarantine != nil {
		refresher.Quarantine.Quarantine(refresher.Upstream, "contract changed")
	}
	refresher.lastValid = now
	return nil
}
