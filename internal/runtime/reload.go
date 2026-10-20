package runtime

import (
	"context"
	"errors"
	"sync/atomic"

	"github.com/mcpgate/mcpgate/internal/contracts"
)

var ErrRestartRequired = errors.New("listener topology changed; restart required")

func (coordinator *Coordinator) Reload(ctx context.Context) error {
	coordinator.operation.Lock()
	defer coordinator.operation.Unlock()
	candidate, err := coordinator.options.Builder.Build(ctx)
	if err != nil {
		return err
	}
	if candidate.Generation == nil {
		return ErrInvalidGeneration
	}
	if coordinator.topology != "" && candidate.ListenerTopology != coordinator.topology {
		disposeGeneration(candidate.Generation)
		return ErrRestartRequired
	}
	if coordinator.options.Activate != nil {
		if err := coordinator.options.Activate(ctx, candidate); err != nil {
			disposeGeneration(candidate.Generation)
			return err
		}
	}
	if err := coordinator.options.Store.Publish(ctx, candidate.Generation); err != nil {
		return err
	}
	previous := coordinator.Status()
	previous.Ready = true
	previous.Reason = ""
	previous.Revision = candidate.Generation.Revision()
	coordinator.topology = candidate.ListenerTopology
	coordinator.setStatus(previous)
	return nil
}

type RevocationSnapshot struct {
	Epoch      uint64
	Principals map[string]struct{}
	Exports    map[contracts.RouteKey]struct{}
	Upstreams  map[string]struct{}
}
type Revocations struct {
	epoch    atomic.Uint64
	snapshot atomic.Pointer[RevocationSnapshot]
}

func NewRevocations() *Revocations {
	overlay := &Revocations{}
	overlay.Publish(nil, nil, nil)
	return overlay
}
func (overlay *Revocations) Publish(principals []string, exports []contracts.RouteKey, upstreams []string) uint64 {
	epoch := overlay.epoch.Add(1)
	snapshot := &RevocationSnapshot{Epoch: epoch, Principals: make(map[string]struct{}), Exports: make(map[contracts.RouteKey]struct{}), Upstreams: make(map[string]struct{})}
	for _, value := range principals {
		snapshot.Principals[value] = struct{}{}
	}
	for _, value := range exports {
		snapshot.Exports[value] = struct{}{}
	}
	for _, value := range upstreams {
		snapshot.Upstreams[value] = struct{}{}
	}
	overlay.snapshot.Store(snapshot)
	return epoch
}
func (overlay *Revocations) Snapshot() RevocationSnapshot {
	source := overlay.snapshot.Load()
	if source == nil {
		return RevocationSnapshot{}
	}
	result := RevocationSnapshot{Epoch: source.Epoch, Principals: make(map[string]struct{}, len(source.Principals)), Exports: make(map[contracts.RouteKey]struct{}, len(source.Exports)), Upstreams: make(map[string]struct{}, len(source.Upstreams))}
	for key := range source.Principals {
		result.Principals[key] = struct{}{}
	}
	for key := range source.Exports {
		result.Exports[key] = struct{}{}
	}
	for key := range source.Upstreams {
		result.Upstreams[key] = struct{}{}
	}
	return result
}
