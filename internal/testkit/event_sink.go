package testkit

import (
	"context"
	"sync"

	"github.com/9OctopusDev/mcpgate/internal/contracts"
)

type EventSink struct {
	mu     sync.Mutex
	Events []contracts.ProgressEvent
	Err    error
}

func (sink *EventSink) Progress(_ context.Context, event contracts.ProgressEvent) error {
	sink.mu.Lock()
	defer sink.mu.Unlock()
	sink.Events = append(sink.Events, event)
	return sink.Err
}

func (sink *EventSink) Snapshot() []contracts.ProgressEvent {
	sink.mu.Lock()
	defer sink.mu.Unlock()
	return append([]contracts.ProgressEvent(nil), sink.Events...)
}
