package runtime

import (
	"context"
	"time"
)

func (coordinator *Coordinator) Shutdown(parent context.Context) (result error) {
	coordinator.shutdown.Do(func() {
		coordinator.operation.Lock()
		defer coordinator.operation.Unlock()
		coordinator.setStatus(Status{Reason: "shutting down"})
		if coordinator.options.StopAdmission != nil {
			coordinator.options.StopAdmission()
		}
		if coordinator.options.CancelControl != nil {
			coordinator.options.CancelControl()
		}
		ctx, cancel := context.WithTimeout(parent, coordinator.options.ShutdownTimeout)
		defer cancel()
		if err := coordinator.options.Store.Shutdown(ctx); err != nil {
			result = err
		}
		if coordinator.options.CloseResources != nil {
			if err := coordinator.options.CloseResources(ctx); err != nil && result == nil {
				result = err
			}
		}
		coordinator.setStatus(Status{Reason: "stopped"})
	})
	return result
}

func (store *Store) Shutdown(ctx context.Context) error {
	store.mu.Lock()
	state := store.active
	store.active = nil
	if state == nil {
		store.mu.Unlock()
		return nil
	}
	state.accepting = false
	if state.refs.Load() == 0 {
		state.finalized = true
		store.mu.Unlock()
		disposeGeneration(state.generation)
		return nil
	}
	store.mu.Unlock()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			store.expire(state)
			return ctx.Err()
		case <-ticker.C:
			store.mu.Lock()
			done := state.finalized || state.refs.Load() == 0
			if done && !state.finalized {
				state.finalized = true
				if state.timer != nil {
					state.timer.Stop()
				}
			}
			store.mu.Unlock()
			if done {
				if state.refs.Load() == 0 {
					disposeGeneration(state.generation)
				}
				return nil
			}
		}
	}
}
