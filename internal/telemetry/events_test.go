package telemetry

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestEventSinkRedactsSensitiveFields(t *testing.T) {
	var output bytes.Buffer
	sink := NewJSONEventSink(&output)
	sink.Emit(context.Background(), Event{Name: "dispatch", Fields: map[string]any{
		"server": "public", "authorization": "Bearer secret-token", "arguments": `{"password":"secret"}`,
	}})
	text := output.String()
	if strings.Contains(text, "secret-token") || strings.Contains(text, "password") || !strings.Contains(text, "[REDACTED]") {
		t.Fatalf("unsafe event output: %s", text)
	}
}
