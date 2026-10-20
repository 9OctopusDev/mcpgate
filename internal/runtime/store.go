package runtime

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

var (
	ErrNoActiveGeneration = errors.New("no active runtime generation")
	ErrInvalidGeneration  = errors.New("runtime generation is nil")
)

type generationState struct {
	generation *Generation
	refs       atomic.Int64
	accepting  bool
	finalized  bool
	timer      *time.Timer
}

type Store struct {
	mu     sync.RWMutex
	active *generationState
}

func NewStore(initial *Generation) *Store {
	store := &Store{}
	if initial != nil {
		store.active = &generationState{generation: initial, accepting: true}
	}
	return store
}

func (store *Store) Acquire() (*Lease, error) {
	store.mu.RLock()
	state := store.active
	if state == nil || !state.accepting {
		store.mu.RUnlock()
		return nil, ErrNoActiveGeneration
	}
	state.refs.Add(1)
	store.mu.RUnlock()
	return &Lease{store: store, state: state}, nil
}

func (store *Store) Publish(ctx context.Context, candidate *Generation) error {
	if candidate == nil {
		return ErrInvalidGeneration
	}
	if err := ctx.Err(); err != nil {
		disposeGeneration(candidate)
		return err
	}

	var closeNow *Generation
	store.mu.Lock()
	if err := ctx.Err(); err != nil {
		store.mu.Unlock()
		disposeGeneration(candidate)
		return err
	}
	previous := store.active
	store.active = &generationState{generation: candidate, accepting: true}
	if previous != nil {
		previous.accepting = false
		if previous.refs.Load() == 0 {
			previous.finalized = true
			closeNow = previous.generation
		} else {
			drain := previous.generation.DrainTimeout()
			if drain < 0 {
				drain = 0
			}
			previous.timer = time.AfterFunc(drain, func() { store.expire(previous) })
		}
	}
	store.mu.Unlock()
	if closeNow != nil {
		disposeGeneration(closeNow)
	}
	return nil
}

func (store *Store) expire(state *generationState) {
	store.mu.Lock()
	if state.finalized {
		store.mu.Unlock()
		return
	}
	state.finalized = true
	store.mu.Unlock()
	disposeGeneration(state.generation)
}

func (store *Store) release(state *generationState) {
	var closeNow *Generation
	store.mu.Lock()
	remaining := state.refs.Add(-1)
	if remaining < 0 {
		state.refs.Store(0)
		remaining = 0
	}
	if !state.accepting && remaining == 0 && !state.finalized {
		state.finalized = true
		if state.timer != nil {
			state.timer.Stop()
		}
		closeNow = state.generation
	}
	store.mu.Unlock()
	if closeNow != nil {
		disposeGeneration(closeNow)
	}
}

func disposeGeneration(generation *Generation) {
	timeout := generation.DrainTimeout()
	if timeout <= 0 {
		timeout = time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	generation.close(ctx)
}
