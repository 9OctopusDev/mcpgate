package rest

import (
	"encoding/json"
	"errors"
)

var (
	ErrDocumentTooLarge     = errors.New("OpenAPI document exceeds configured byte limit")
	ErrUnsupportedOpenAPI   = errors.New("unsupported OpenAPI construct")
	ErrOperationNotFound    = errors.New("OpenAPI operationId not found")
	ErrDuplicateOperationID = errors.New("duplicate OpenAPI operationId")
	ErrExternalReference    = errors.New("external OpenAPI reference is disabled")
	ErrSecurityUnsatisfied  = errors.New("OpenAPI security requirement is not satisfied")
	ErrInvalidParameter     = errors.New("invalid REST parameter")
)

type CompileOptions struct {
	OperationID      string
	EndpointBase     string
	SecurityBindings map[string]bool
	MaxDocumentBytes int
}

type SegmentKind uint8

const (
	LiteralSegment SegmentKind = iota
	ParameterSegment
)

type PathSegment struct {
	Kind  SegmentKind
	Value string
}

type ParameterPlan struct {
	Name     string
	Location string
	Required bool
	Array    bool
	Explode  bool
	Nullable bool
	Schema   json.RawMessage
}

type BodyPlan struct {
	Required bool
	Schema   json.RawMessage
}

type RESTPlan struct {
	OperationID     string
	Method          string
	EndpointBase    string
	PathTemplate    string
	PathSegments    []PathSegment
	PathParameters  []ParameterPlan
	QueryParameters []ParameterPlan
	Body            *BodyPlan
	InputSchema     json.RawMessage
	ResponseSchemas map[string]json.RawMessage
	SecuritySchemes []string
	ReadOnly        bool
}
