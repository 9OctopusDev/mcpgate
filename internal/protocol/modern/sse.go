package modern

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/9OctopusDev/mcpgate/internal/contracts"
)

type sseSink struct {
	ctx    context.Context
	events chan contracts.ProgressEvent
}

func (sink *sseSink) Progress(ctx context.Context, event contracts.ProgressEvent) error {
	select {
	case <-sink.ctx.Done():
		return sink.ctx.Err()
	case <-ctx.Done():
		return ctx.Err()
	case sink.events <- event:
		return nil
	default:
		return errors.New("progress queue capacity exceeded")
	}
}

func writeSSE(writer http.ResponseWriter, request *http.Request, execute func(contracts.EventSink) (contracts.Outcome, error), id json.RawMessage) {
	writer.Header().Set("Content-Type", "text/event-stream")
	writer.Header().Set("Cache-Control", "no-store")
	writer.WriteHeader(http.StatusOK)
	if flusher, ok := writer.(http.Flusher); ok {
		_, _ = writer.Write([]byte(": mcpgate\n\n"))
		flusher.Flush()
	}
	sink := &sseSink{ctx: request.Context(), events: make(chan contracts.ProgressEvent, 32)}
	type completed struct {
		outcome contracts.Outcome
		err     error
	}
	done := make(chan completed, 1)
	go func() { outcome, err := execute(sink); done <- completed{outcome: outcome, err: err} }()
	encoder := json.NewEncoder(writer)
	for {
		select {
		case <-request.Context().Done():
			return
		case event := <-sink.events:
			_, _ = writer.Write([]byte("event: progress\ndata: "))
			_ = encoder.Encode(event)
			_, _ = writer.Write([]byte("\n"))
			if flusher, ok := writer.(http.Flusher); ok {
				flusher.Flush()
			}
		case result := <-done:
			var envelope response
			if result.err != nil {
				envelope = errorResponse(id, -32603, "tool execution failed")
			} else {
				envelope = outcomeResponse(id, result.outcome)
			}
			_, _ = writer.Write([]byte("event: result\ndata: "))
			_ = encoder.Encode(envelope)
			_, _ = writer.Write([]byte("\n"))
			return
		}
	}
}
