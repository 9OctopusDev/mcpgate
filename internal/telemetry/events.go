package telemetry

import (
	"context"
	"encoding/json"
	"io"
	"sync"
	"time"
)

type Event struct {
	Name   string         `json:"name"`
	Time   time.Time      `json:"time"`
	Fields map[string]any `json:"fields,omitempty"`
}

type JSONEventSink struct {
	mu     sync.Mutex
	writer io.Writer
	now    func() time.Time
}

func NewJSONEventSink(writer io.Writer) *JSONEventSink {
	return &JSONEventSink{writer: writer, now: time.Now}
}

func (sink *JSONEventSink) Emit(_ context.Context, event Event) {
	if sink == nil || sink.writer == nil {
		return
	}
	if event.Time.IsZero() {
		event.Time = sink.now().UTC()
	}
	fields := make(map[string]any, len(event.Fields))
	for key, value := range event.Fields {
		if sensitive(key) {
			fields[key] = "[REDACTED]"
		} else {
			fields[key] = value
		}
	}
	event.Fields = fields
	sink.mu.Lock()
	defer sink.mu.Unlock()
	_ = json.NewEncoder(sink.writer).Encode(event)
}
