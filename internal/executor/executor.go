package executor

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/9OctopusDev/mcpgate/internal/contracts"
)

var (
	ErrDenied             = errors.New("tool is not available")
	ErrAdapterUnavailable = errors.New("upstream adapter unavailable")
)

type Request struct {
	Route       contracts.Route
	Principal   contracts.Principal
	Arguments   json.RawMessage
	RequestID   json.RawMessage
	SessionID   string
	TraceID     string
	Deadline    time.Time
	BufferBytes int64
}

type AdapterRegistry interface {
	Adapter(string) (contracts.ToolAdapter, bool)
}
type StaticAdapters map[string]contracts.ToolAdapter

func (adapters StaticAdapters) Adapter(id string) (contracts.ToolAdapter, bool) {
	adapter, ok := adapters[id]
	return adapter, ok
}

type AuditEvent struct {
	Phase, ExecutionID, TraceID, ServerID, Tool, NativeName, UpstreamID, Principal, RouteRevision string
	PolicyEpoch                                                                                   uint64
	Attempt                                                                                       int
	At                                                                                            time.Time
	Outcome                                                                                       string
}
type AuditSink interface {
	Record(context.Context, AuditEvent)
}
type nopAudit struct{}

func (nopAudit) Record(context.Context, AuditEvent) {}

type Dependencies struct {
	Authorizer contracts.Authorizer
	Adapters   AdapterRegistry
	Audit      AuditSink
}

type Executor struct {
	deps         Dependencies
	limits       Limits
	admission    *admission
	correlations *Correlations
	now          func() time.Time
}

func New(dependencies Dependencies, limits Limits) (*Executor, error) {
	if dependencies.Authorizer == nil || dependencies.Adapters == nil {
		return nil, fmt.Errorf("authorizer and adapter registry are required")
	}
	if limits.Active <= 0 || limits.Queued <= 0 || limits.QueueWait <= 0 || limits.BufferBytes <= 0 || limits.PerPrincipalRate <= 0 || limits.PerPrincipalBurst <= 0 {
		return nil, fmt.Errorf("invalid executor limits")
	}
	if dependencies.Audit == nil {
		dependencies.Audit = nopAudit{}
	}
	return &Executor{deps: dependencies, limits: limits, admission: newAdmission(limits), correlations: NewCorrelations(), now: time.Now}, nil
}

func (executor *Executor) Active() int64                  { return executor.admission.inflight.Load() }
func (executor *Executor) Cancel(key CorrelationKey) bool { return executor.correlations.Cancel(key) }

func (executor *Executor) Execute(parent context.Context, request Request, sink contracts.EventSink) (outcome contracts.Outcome, err error) {
	machine := newStateMachine()
	if request.Principal.PartitionDigest == "" {
		return contracts.Outcome{}, ErrDenied
	}
	decision, err := executor.deps.Authorizer.Authorize(parent, request.Principal, request.Route)
	if err != nil || !decision.Allowed {
		return contracts.Outcome{}, ErrDenied
	}
	_ = machine.move(StateAuthorized)
	if !executor.admission.allowPrincipal(request.Principal.PartitionDigest, executor.now()) {
		return contracts.Outcome{}, ErrRateLimited
	}
	_ = machine.move(StateQueued)
	ctx := parent
	var cancel context.CancelFunc
	if !request.Deadline.IsZero() {
		ctx, cancel = context.WithDeadline(parent, request.Deadline)
	} else {
		ctx, cancel = context.WithCancel(parent)
	}
	defer cancel()
	releaseActive, err := executor.admission.acquire(ctx, executor.limits.QueueWait)
	if err != nil {
		return contracts.Outcome{}, err
	}
	defer releaseActive()
	reservation := request.BufferBytes
	if reservation <= 0 {
		reservation = int64(len(request.Arguments)) + 4096
		if reservation > executor.limits.BufferBytes {
			reservation = executor.limits.BufferBytes
		}
	}
	releaseBuffer, err := executor.admission.buffer.reserve(reservation)
	if err != nil {
		return contracts.Outcome{}, err
	}
	defer releaseBuffer()
	adapter, ok := executor.deps.Adapters.Adapter(request.Route.UpstreamID)
	if !ok {
		return contracts.Outcome{}, ErrAdapterUnavailable
	}
	rechecked, err := executor.deps.Authorizer.RecheckAtDispatch(ctx, request.Principal, request.Route, decision.Epoch)
	if err != nil || !rechecked.Allowed {
		return contracts.Outcome{}, ErrDenied
	}
	_ = machine.move(StateDispatching)
	executionID := randomID()
	key := CorrelationKey{Principal: request.Principal.PartitionDigest, Session: request.SessionID, Request: string(request.RequestID)}
	executor.correlations.Track(key, executionID, cancel)
	defer executor.correlations.Untrack(key, executionID)
	call := contracts.Call{ExecutionID: executionID, Route: request.Route, Principal: request.Principal, Arguments: append(json.RawMessage(nil), request.Arguments...), RequestID: append(json.RawMessage(nil), request.RequestID...), Deadline: request.Deadline}
	_ = machine.move(StateRunning)
	for attempt := 1; ; attempt++ {
		event := executor.auditEvent(request, executionID, rechecked.Epoch, attempt, "dispatch", "")
		executor.deps.Audit.Record(ctx, event)
		outcome, err = adapter.CallTool(ctx, call, sink)
		if err == nil {
			if validation := outcome.Validate(); validation != nil {
				err = validation
			} else {
				_ = machine.move(StateComplete)
				executor.deps.Audit.Record(ctx, executor.auditEvent(request, executionID, rechecked.Epoch, attempt, "complete", classify(outcome)))
				return outcome, nil
			}
		}
		if !mayRetry(request.Route, attempt, err) {
			break
		}
		if err := waitRetry(ctx, attempt); err != nil {
			break
		}
		rechecked, err = executor.deps.Authorizer.RecheckAtDispatch(ctx, request.Principal, request.Route, rechecked.Epoch)
		if err != nil || !rechecked.Allowed {
			return contracts.Outcome{}, ErrDenied
		}
	}
	if ctx.Err() != nil {
		_ = machine.move(StateCanceled)
	} else {
		_ = machine.move(StateFailed)
	}
	executor.deps.Audit.Record(context.Background(), executor.auditEvent(request, executionID, rechecked.Epoch, 0, "complete", "OUTCOME_UNKNOWN"))
	return contracts.Outcome{}, err
}

func (executor *Executor) auditEvent(request Request, executionID string, epoch uint64, attempt int, phase, outcome string) AuditEvent {
	return AuditEvent{Phase: phase, ExecutionID: executionID, TraceID: request.TraceID, ServerID: request.Route.Key.ServerID, Tool: request.Route.Key.PublicName, NativeName: request.Route.NativeName, UpstreamID: request.Route.UpstreamID, Principal: request.Principal.PartitionDigest, RouteRevision: request.Route.Contract.Fingerprint, PolicyEpoch: epoch, Attempt: attempt, At: executor.now(), Outcome: outcome}
}

func randomID() string {
	var data [16]byte
	if _, err := rand.Read(data[:]); err != nil {
		return fmt.Sprintf("fallback-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(data[:])
}
func classify(outcome contracts.Outcome) string {
	if outcome.ProtocolError != nil {
		return "PROTOCOL_ERROR"
	}
	if outcome.InputRequired != nil {
		return "INPUT_REQUIRED"
	}
	if outcome.Complete != nil && outcome.Complete.IsError {
		return "BUSINESS_ERROR"
	}
	return "COMPLETE"
}
