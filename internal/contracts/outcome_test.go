package contracts

import (
	"encoding/json"
	"testing"
)

func TestOutcomeValidateRequiresExactlyOneVariant(t *testing.T) {
	complete := Outcome{Complete: &ToolResult{Structured: json.RawMessage(`{"ok":true}`)}}
	if err := complete.Validate(); err != nil {
		t.Fatalf("complete outcome rejected: %v", err)
	}

	if err := (Outcome{}).Validate(); err == nil {
		t.Fatal("empty outcome accepted")
	}

	both := complete
	both.ProtocolError = &ProtocolError{Code: -32603, Message: "failure"}
	if err := both.Validate(); err == nil {
		t.Fatal("ambiguous outcome accepted")
	}
}

func TestNewToolResultCopiesMutableJSON(t *testing.T) {
	input := json.RawMessage(`{"id":9007199254740993}`)
	result := NewToolResult(input, nil, false)
	input[6] = '0'

	if got, want := string(result.Structured), `{"id":9007199254740993}`; got != want {
		t.Fatalf("structured content aliased caller memory: got %s, want %s", got, want)
	}
}
