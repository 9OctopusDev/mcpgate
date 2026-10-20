package contracts

import (
	"encoding/json"
	"errors"
)

var (
	ErrInvalidOutcome                 = errors.New("outcome must contain exactly one variant")
	ErrCredentialOperationUnsupported = errors.New("credential operation is unsupported")
)

type ContentBlock struct {
	Type     string          `json:"type"`
	Text     string          `json:"text,omitempty"`
	Data     []byte          `json:"data,omitempty"`
	MIMEType string          `json:"mimeType,omitempty"`
	URI      string          `json:"uri,omitempty"`
	Metadata json.RawMessage `json:"metadata,omitempty"`
}

type ToolResult struct {
	Structured json.RawMessage            `json:"structuredContent,omitempty"`
	Content    []ContentBlock             `json:"content,omitempty"`
	IsError    bool                       `json:"isError,omitempty"`
	Metadata   map[string]json.RawMessage `json:"metadata,omitempty"`
}

type ProtocolError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

type InputRequired struct {
	State   json.RawMessage            `json:"state"`
	Inputs  map[string]json.RawMessage `json:"inputs"`
	Expires int64                      `json:"expires,omitempty"`
}

type Outcome struct {
	Complete      *ToolResult
	ProtocolError *ProtocolError
	InputRequired *InputRequired
}

func (o Outcome) Validate() error {
	variants := 0
	if o.Complete != nil {
		variants++
	}
	if o.ProtocolError != nil {
		variants++
	}
	if o.InputRequired != nil {
		variants++
	}
	if variants != 1 {
		return ErrInvalidOutcome
	}
	return nil
}

func NewToolResult(structured json.RawMessage, content []ContentBlock, isError bool) *ToolResult {
	result := &ToolResult{
		Structured: cloneBytes(structured),
		Content:    make([]ContentBlock, len(content)),
		IsError:    isError,
	}
	for i := range content {
		result.Content[i] = content[i]
		result.Content[i].Data = cloneBytes(content[i].Data)
		result.Content[i].Metadata = cloneBytes(content[i].Metadata)
	}
	return result
}

func cloneBytes[T ~[]byte](value T) T {
	if value == nil {
		return nil
	}
	return append(T(nil), value...)
}
