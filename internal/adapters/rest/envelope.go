package rest

import (
	"encoding/json"

	"github.com/mcpgate/mcpgate/internal/contracts"
)

type successEnvelope struct {
	OK     bool `json:"ok"`
	Status int  `json:"status"`
	Data   any  `json:"data"`
}

type errorEnvelope struct {
	OK    bool      `json:"ok"`
	Error safeError `json:"error"`
}

type safeError struct {
	Code        string `json:"code"`
	Message     string `json:"message"`
	Status      int    `json:"status,omitempty"`
	ExecutionID string `json:"execution_id,omitempty"`
	Retryable   bool   `json:"retryable"`
	Outcome     string `json:"outcome,omitempty"`
}

func completeEnvelope(value any, isError bool) contracts.Outcome {
	encoded, _ := json.Marshal(value)
	result := contracts.NewToolResult(encoded, []contracts.ContentBlock{{Type: "text", Text: string(encoded)}}, isError)
	return contracts.Outcome{Complete: result}
}

func failureOutcome(code, message string, status int, executionID, certainty string) contracts.Outcome {
	return completeEnvelope(errorEnvelope{OK: false, Error: safeError{
		Code: code, Message: message, Status: status, ExecutionID: executionID, Retryable: false, Outcome: certainty,
	}}, true)
}
