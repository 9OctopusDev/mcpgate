// Package contracts defines the gateway-owned boundaries shared by the
// compiler, protocol facades, policy engine, and upstream adapters.
package contracts

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

type RouteKey struct {
	ServerID   string
	PublicName string
}

type Behavior string

const (
	BehaviorReadOnly Behavior = "read_only"
	BehaviorMutating Behavior = "mutating"
	BehaviorUnknown  Behavior = "unknown"
)

type ToolContract struct {
	Name             string
	Description      string
	InputSchema      json.RawMessage
	OutputSchema     json.RawMessage
	RequiredFeatures []string
	Fingerprint      string
}

type Route struct {
	Key         RouteKey
	UpstreamID  string
	NativeName  string
	PolicyID    string
	Contract    ToolContract
	Behavior    Behavior
	Timeout     time.Duration
	MaxAttempts int
}

type Principal struct {
	Issuer          string
	Subject         string
	Tenant          string
	ClientID        string
	Scopes          []string
	PartitionDigest string
}

type CredentialIdentity struct {
	Reference string
	Epoch     uint64
}

// CredentialBinding exposes operations instead of raw secret bytes. Methods
// return ErrCredentialOperationUnsupported when a binding does not support an
// operation (for example, a bearer binding cannot sign catalog cursors).
type CredentialBinding interface {
	Identity() CredentialIdentity
	ApplyHTTP(*http.Request) error
	Sign([]byte) ([]byte, error)
	Verify(message, signature []byte) error
	Close()
}

type Catalog struct {
	Tools      []ToolContract
	Revision   string
	ValidUntil time.Time
	Protocol   string
}

type Call struct {
	ExecutionID string
	Route       Route
	Principal   Principal
	Arguments   json.RawMessage
	RequestID   json.RawMessage
	Deadline    time.Time
}

type ProgressEvent struct {
	Sequence uint64
	Message  string
	Current  float64
	Total    *float64
}

type EventSink interface {
	Progress(context.Context, ProgressEvent) error
}

type ToolAdapter interface {
	Discover(context.Context, CredentialBinding) (Catalog, error)
	CallTool(context.Context, Call, EventSink) (Outcome, error)
	Close(context.Context) error
}

type Snapshot interface {
	Revision() string
}

type SnapshotLease interface {
	Snapshot() Snapshot
	Release()
}

type SnapshotStore interface {
	Acquire() (SnapshotLease, error)
	Publish(context.Context, Snapshot) error
}

type Decision struct {
	Allowed bool
	Epoch   uint64
	Reason  string
}

type Authorizer interface {
	Authorize(context.Context, Principal, Route) (Decision, error)
	RecheckAtDispatch(context.Context, Principal, Route, uint64) (Decision, error)
}
